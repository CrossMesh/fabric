package metanet

import (
	"context"
	"sync"

	"github.com/crossmesh/fabric/backend"
	gossipUtils "github.com/crossmesh/fabric/gossip"
	"github.com/crossmesh/fabric/proto"
)

// MessageHandler handle incoming message.
type MessageHandler func(*Message)

// Message contains context of message.
type Message struct {
	n *MetadataNetwork

	Endpoint backend.Endpoint
	Via      backend.Endpoint

	peer    *MetaPeer
	TypeID  uint16
	Packed  []byte
	Payload []byte
}

// GetPeerName calculates name of sender.
func (m *Message) GetPeerName() string {
	return gossipUtils.BuildNodeName(m.Endpoint)
}

// Peer reports the message sender.
func (m *Message) Peer() (peer *MetaPeer) {
	if peer = m.peer; peer != nil {
		return peer
	}
	name := gossipUtils.BuildNodeName(m.Endpoint)
	name2Peer := m.n.Publish.Name2Peer
	peer, _ = name2Peer[name]
	m.peer = peer
	return
}

func (n *MetadataNetwork) receiveRemote(b backend.Backend, packed []byte, src string) {
	typeID, payload := proto.UnpackProtocolMessageHeader(packed)
	rh, hasHandler := n.messageHandlers.Load(typeID)
	if !hasHandler || rh == nil {
		return
	}
	handler, isHandler := rh.(MessageHandler)
	if !isHandler || handler == nil {
		return
	}
	msg := Message{
		n:       n,
		Packed:  packed,
		Payload: payload,
		TypeID:  typeID,
		Endpoint: backend.Endpoint{
			Type:     b.Type(),
			Endpoint: src,
		},
		Via: backend.Endpoint{
			Type:     b.Type(),
			Endpoint: b.Publish(),
		},
	}
	handler(&msg)
}

// RegisterMessageHandler registers message handler for specific message type.
func (n *MetadataNetwork) RegisterMessageHandler(typeID uint16, handler MessageHandler) MessageHandler {
	rv, found := n.messageHandlers.Load(typeID)
	for {
		if handler == nil && !found {
			break
		}
		actual, stored := n.messageHandlers.LoadOrStore(typeID, handler)
		if stored {
			break
		}
		rv = actual
	}
	if rv == nil {
		return nil
	}
	return rv.(MessageHandler)
}

// SendToPeers sends a message to peers.
func (n *MetadataNetwork) SendToPeers(typeID uint16, payload []byte, peers ...*MetaPeer) {
	if len(peers) < 1 {
		return
	}

	packed := make([]byte, proto.ProtocolMessageHeaderSize, len(payload)+proto.ProtocolMessageHeaderSize)
	proto.PackProtocolMessageHeader(packed[:proto.ProtocolMessageHeaderSize], typeID)
	packed = append(packed, payload...)

	// TODO(xutao): deliver directly if a message is sent to self.

	for _, peer := range peers {
		endpoint := peer.chooseEndpoint()
		if endpoint.Type == backend.UnknownBackend {
			continue
		}
		if err := n.nakedSendToEndpoint(packed, endpoint); err != nil {
			n.lastFails.Store(endpoint, peer)
		}
	}
}

// SendToNames sends a message to peers with specific names.
func (n *MetadataNetwork) SendToNames(typeID uint16, payload []byte, names ...string) {
	if len(names) < 1 {
		return
	}

	name2Peer := n.Publish.Name2Peer
	if len(names) == 1 {
		p, _ := name2Peer[names[0]]
		if p == nil {
			return
		}
		n.SendToPeers(typeID, payload, p)
		return
	}

	hit := make(map[*MetaPeer]struct{}, 1)
	for _, name := range names {
		p, _ := name2Peer[name]
		if p == nil {
			continue
		}
		hit[p] = struct{}{}
	}
	if len(hit) < 1 {
		return
	}
	peers := make([]*MetaPeer, 0, len(hit))
	for peer := range hit {
		peers = append(peers, peer)
	}
	n.SendToPeers(typeID, payload, peers...)
}

// SendToEndpoints sends a message to endpoints.
func (n *MetadataNetwork) SendToEndpoints(typeID uint16, payload []byte, endpoints ...backend.Endpoint) {
	bins := make([]byte, proto.ProtocolMessageHeaderSize, len(payload)+proto.ProtocolMessageHeaderSize)
	proto.PackProtocolMessageHeader(bins[:proto.ProtocolMessageHeaderSize], typeID)
	bins = append(bins, payload...)
	for _, endpoint := range endpoints {
		n.nakedSendToEndpoint(bins, endpoint)
	}
}

// SendViaEndpoint sends a message via given endpoint.
func (n *MetadataNetwork) SendViaEndpoint(typeID uint16, payload []byte, via backend.Endpoint, to string) {
	n.lock.RLock()
	backend, hasBackend := n.backends[via]
	n.lock.RUnlock()
	if !hasBackend || backend == nil {
		return
	}

	packed := make([]byte, proto.ProtocolMessageHeaderSize, len(payload)+proto.ProtocolMessageHeaderSize)
	proto.PackProtocolMessageHeader(packed[:proto.ProtocolMessageHeaderSize], typeID)
	packed = append(packed, payload...)

	n.nakedSendViaBackend(packed, backend, to)
}

func (n *MetadataNetwork) nakedSendViaBackend(packed []byte, b backend.Backend, to string) error {
	// TODO(xutao): implement reliable sending.
	link, err := b.Connect(to)
	if err != nil {
		if err != backend.ErrOperationCanceled {
			n.log.Errorf("failed to get link. (err = \"%v\")", err)
		} else {
			err = nil
		}
		return err
	}
	if err = link.Send(packed); err != nil {
		if err != backend.ErrOperationCanceled {
			n.log.Errorf("failed to send packet. (err = \"%v\")", err)
		} else {
			err = nil
		}
	}
	return err
}

func (n *MetadataNetwork) nakedSendToEndpoint(packed []byte, endpoint backend.Endpoint) error {
	rv, hasBackend := n.Publish.Type2Backend.Load(endpoint.Type)
	if !hasBackend || rv == nil {
		return nil
	}
	b, isBackend := rv.(backend.Backend)
	if !isBackend {
		return nil
	}
	return n.nakedSendViaBackend(packed, b, endpoint.Endpoint)
}

type gossipEngineMessage struct {
	name    string
	payload []byte
}

type gossipEngineTransport struct {
	n       *MetadataNetwork
	bufChan chan *gossipEngineMessage
	msgPool *sync.Pool
}

func (n *MetadataNetwork) newGossipEngineTransport(maxSize uint) (t *gossipEngineTransport) {
	t = &gossipEngineTransport{
		n:       n,
		bufChan: make(chan *gossipEngineMessage, maxSize),
		msgPool: &sync.Pool{
			New: func() interface{} { return &gossipEngineMessage{} },
		},
	}
	n.RegisterMessageHandler(proto.MsgTypeGossip, t.putMessage)
	return
}

func (t *gossipEngineTransport) putMessage(m *Message) {
	msg := t.msgPool.Get().(*gossipEngineMessage)
	if msg.payload == nil {
		msg.payload = make([]byte, 0, len(m.Payload))
	} else {
		msg.payload = msg.payload[:0]
	}
	msg.payload = append(msg.payload, m.Payload...)
	msg.name = m.GetPeerName()

	select { // send or drop.
	case t.bufChan <- msg:
	default:
		t.msgPool.Put(msg)
	}
}

func (t *gossipEngineTransport) Receive(ctx context.Context) (names []string, payload []byte) {
	select {
	case msg := <-t.bufChan:
		payload, names = msg.payload, []string{msg.name}
		t.n.log.Debug("get a gossip message from ", msg.name, ", payload len = ", len(msg.payload))

		t.msgPool.Put(msg)
	case <-ctx.Done():
		return nil, nil
	}
	return
}

func (t *gossipEngineTransport) Send(names []string, payload []byte) {
	t.n.SendToNames(proto.MsgTypeGossip, payload, names...)
	t.n.log.Debug("send a gossip message to ", names, ", payload len = ", len(payload))
}
