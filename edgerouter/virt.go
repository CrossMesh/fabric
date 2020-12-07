package edgerouter

import (
	"strconv"

	"github.com/crossmesh/fabric/edgerouter/driver"
	"github.com/crossmesh/netns"
)

func namedNSName(id int32) string {
	return "vnet" + strconv.FormatUint(uint64(id), 16)
}

type netnsBinding struct {
	id int32
	ns netns.NsHandle
}

type virtualNetworkDoContext struct {
	underlay netnsBinding
	current  netnsBinding
}

type virtualNetworkDoFibre struct {
	do         func(*virtualNetworkDoContext) error
	netID      int32
	ignoreLock bool

	completeC chan error
}

func (r *EdgeRouter) submitVirtualDo(netID int32, ignoreLock bool, process func(*virtualNetworkDoContext) error) <-chan error {
	completeC := make(chan error)

	r.virtualDoC <- &virtualNetworkDoFibre{
		do:         process,
		netID:      netID,
		completeC:  make(chan error),
		ignoreLock: ignoreLock,
	}

	return completeC
}

func (r *EdgeRouter) startProcessVirtualDo() error {
	var origin netns.NsHandle
	var err error

	if origin, err = netns.Get(); err != nil {
		r.log.Errorf("cannot open underlay working netns. (err = \"%v\")", err)
		return err
	}

	r.arbiters.main.Do(func() {
		opened := make(map[int32]netns.NsHandle)

		openNetNs := func(netID int32) (netns.NsHandle, error) {
			ns, isOpened := opened[netID]
			if isOpened && ns.IsOpen() {
				return ns, nil
			}

			if ns, err = r.netns.Open(namedNSName(netID)); err != nil {
				return ns, nil
			}
			opened[netID] = ns

			return ns, nil
		}

		freeNetNs := func(netID int32, ns netns.NsHandle) {
			if !ns.IsOpen() {
				return
			}

			err := ns.Close()

			oldNs, isOpened := opened[netID]
			if !isOpened || oldNs != ns { // should not happen.
				r.log.Warnf("[BUG!] free a not traced netns handle. [netID = %v, fd = %v, old_fd = %v]", netID, ns, oldNs)
				return
			}

			if err != nil {
				r.log.Warnf("failed to close netns handle. retry later. [netID = %v, fd = %v] (err = \"%v\")", netID, ns, err)
				return
			}
			delete(opened, netID)
		}

		defer func() { // close all netns fd.
			if err = origin.Close(); err != nil {
				r.log.Fatalf("cannot close origin netns handle. fd may leaks. [fd = %v] (err = \"%v\")", origin, err)
			}
			for netID, ns := range opened {
				if err = ns.Close(); err != nil {
					r.log.Fatalf("cannot close netns handle. fd may leaks. [netID = %v, fd = %v] (err = \"%v\")", netID, origin, err)
				}
			}
		}()

		var virtDoFibre *virtualNetworkDoFibre

	processVirtualDo:
		for r.arbiters.main.ShouldRun() {
			select {
			case <-r.arbiters.main.Exit():
				break processVirtualDo
			case virtDoFibre = <-r.virtualDoC:
			}

			if virtDoFibre == nil || virtDoFibre.do == nil {
				continue
			}

			err = nil

			for {
				if !virtDoFibre.ignoreLock {
					r.lock.RLock()
				}

				underlayID := r.underlay.ID

				if virtDoFibre.netID != driver.NetIDUnderlay &&
					underlayID != virtDoFibre.netID { // virtualized.
					var ns netns.NsHandle

					if ns, err = openNetNs(virtDoFibre.netID); err != nil {
						break
					}

					if err = netns.Set(ns); err != nil { // switch netns.
						break
					}

					err = virtDoFibre.do(&virtualNetworkDoContext{
						underlay: netnsBinding{
							id: underlayID, ns: origin,
						},
						current: netnsBinding{
							id: virtDoFibre.netID, ns: ns,
						},
					})

					if recoverErr := netns.Set(origin); recoverErr != nil { // switch origin netns.
						// fatal. cannot back to original network namespace.
						panic(recoverErr)
					}

					freeNetNs(virtDoFibre.netID, ns)

				} else { // not virtualized.
					err = virtDoFibre.do(&virtualNetworkDoContext{
						underlay: netnsBinding{
							id: underlayID, ns: origin,
						},
						current: netnsBinding{
							id: underlayID, ns: origin,
						},
					})
				}

				if !virtDoFibre.ignoreLock {
					r.lock.RUnlock()
				}

				break
			}

			completeC := virtDoFibre.completeC
			if completeC != nil {
				completeC <- err
			}
		}
	})

	return nil
}

func (r *EdgeRouter) ensureNetworkNamespace(netID int32) error {
	if netID == r.underlay.ID { // underlay.
		return nil
	}

	nsName := namedNSName(netID)

	// ns will be in closed state in case of any error, so err can be skipped here?
	if ns, _ := r.netns.Open(nsName); ns.IsOpen() {
		if err := ns.Close(); err != nil {
			return err
		}
		return nil
	}

	return <-r.submitVirtualDo(driver.NetIDUnderlay, true, func(ctx *virtualNetworkDoContext) error {
		ns, err := r.netns.NewNamed(nsName)
		if err != nil {
			return err
		}
		if err = ns.Close(); err != nil {
			return err
		}

		// recover origin netns.
		if err = netns.Set(ctx.current.ns); err != nil {
			panic(err)
		}

		return nil
	})
}
