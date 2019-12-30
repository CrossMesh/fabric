package relay

import (
	"io"
	"net"
	"syscall"
	"time"

	"git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/mux"
	log "github.com/sirupsen/logrus"
)

const (
	defaultBufferSize = 4096
)

type MuxRelay struct {
	in       net.Conn
	listenTo *net.UDPAddr
	muxer    *mux.StreamMuxer
	log      *log.Entry
}

func NewMuxRelay(in net.Conn, listenTo *net.UDPAddr, log *log.Entry) *MuxRelay {
	return &MuxRelay{
		in:       in,
		listenTo: listenTo,
		muxer:    mux.NewStreamMuxer(in),
		log:      log,
	}
}

func (r *MuxRelay) Do(arbiter *arbiter.Arbiter) error {
	buf := make([]byte, 65536)
	oob := make([]byte, 16)

	if r.log != nil {
		r.log.Infof("relay start.")
	}
	var (
		udpConn *net.UDPConn
		err     error
	)
	for arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil
		if udpConn, err = net.ListenUDP("udp", r.listenTo); err != nil {
			r.log.Error("udp listen error: ", err)
			continue
		}
		break
	}

	for arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		if err = udpConn.SetReadDeadline(time.Now().Add(time.Second * 5)); err != nil {
			r.log.Error("SetReadDeadline error: ", err)
			continue
		}
		read, _, flag, _, err := udpConn.ReadMsgUDP(buf, oob)
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				err = nil
				continue
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
		if flag&syscall.MSG_TRUNC != 0 {
			if r.log != nil {
				r.log.Warn("drop truncated UDP frame.")
				continue
			}
		}
		if _, err = r.muxer.Mux(buf[:read]); err != nil {
			r.log.Info("mux error: ", err)
		}
	}

	if r.log != nil {
		r.log.Infof("relay stop.")
	}

	return nil
}

type DemuxRelay struct {
	in      net.Conn
	out     net.PacketConn
	demuxer *mux.StreamDemuxer
	relayTo *net.UDPAddr
	log     *log.Entry
}

func NewDemuxRelay(in net.Conn, out net.PacketConn, relayTo *net.UDPAddr, log *log.Entry) *DemuxRelay {
	return &DemuxRelay{
		in:      in,
		out:     out,
		demuxer: mux.NewStreamDemuxer(),
		relayTo: relayTo,
		log:     log,
	}
}

func (r *DemuxRelay) Do(arbiter *arbiter.Arbiter) error {
	buf := make([]byte, defaultBufferSize)
	r.log.Info("relay start.")

	var (
		err  error
		read int
	)
	for arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		if err = r.in.SetReadDeadline(time.Now().Add(time.Second * 5)); err != nil {
			r.log.Error("SetReadDeadline error: ", err)
			continue
		}
		read, err = r.in.Read(buf)
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				err = nil
				continue
			}
			if err == io.EOF {
				return nil
			}
			// flush demuxer to prevent data corrution.
			r.demuxer.Reset()
			return err
		}

		r.demuxer.Demux(buf[:read], func(pkt []byte) {
			written := 0
			if written, err = r.out.WriteTo(pkt, r.relayTo); err != nil {
				r.log.Error("error when relay packet: ", err)
			}
			r.log.Warnf("receive %v bytes packet, but %v relayed.", len(pkt), written)
		})
	}

	r.log.Info("relay stop.")
	return nil
}
