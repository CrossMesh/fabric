package edgerouter

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/crossmesh/fabric/edgerouter/driver"
	"github.com/crossmesh/fabric/metanet"
	"github.com/crossmesh/fabric/proto"
)

func (r *EdgeRouter) watchForDriverMessage(driverType driver.OverlayDriverType, handler driver.MessageHandler) driver.MessageHandler {
	r.lock.Lock()
	defer r.lock.Unlock()

	new := make(map[driver.OverlayDriverType]driver.MessageHandler, len(r.driverMessageHandler))
	for ty, handler := range r.driverMessageHandler {
		new[ty] = handler
	}

	oldHandler, _ := new[driverType]
	new[driverType] = handler

	r.driverMessageHandler = new

	return oldHandler
}

func (r *EdgeRouter) processDriverMessage(msg *metanet.Message) {
	handlers := r.driverMessageHandler

	if len(msg.Payload) < 1 {
		r.log.Warn("got a driver message which is too short. drop.")
		return
	}
	driverType := driver.OverlayDriverType(binary.BigEndian.Uint16(msg.Payload[:2]))

	handler, _ := handlers[driverType]
	if handler == nil {
		return
	}

	payload := msg.Payload[2:]

	handler(msg.Peer(), payload)
}

func (r *EdgeRouter) fastSendVNetDriverMessage(peer *metanet.MetaPeer, driverType driver.OverlayDriverType, writePayload func(io.Writer)) {
	buf := r.bufCache.Get().([]byte)
	if buf != nil {
		buf = buf[:0]
	}
	buffer := bytes.NewBuffer(buf)

	{
		// encode driver type id.
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:2], uint16(driverType))
		buffer.Write(buf[:2])
	}
	writePayload(buffer) // payload

	r.metaNet.SendToPeers(proto.MsgTypeVNetDriver, buffer.Bytes(), peer)

	r.bufCache.Put(buffer.Bytes())
}

//func (r *EdgeRouter) processVNetControllerMessage(msg *metanet.Message) {
//}
