// Package memory provides a memory broker
package memory

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/unistack-org/micro/v3/broker"
	"github.com/unistack-org/micro/v3/logger"
	maddr "github.com/unistack-org/micro/v3/util/addr"
	mnet "github.com/unistack-org/micro/v3/util/net"
)

type memoryBroker struct {
	opts broker.Options

	addr string
	sync.RWMutex
	connected   bool
	Subscribers map[string][]*memorySubscriber
}

type memoryEvent struct {
	opts    broker.Options
	topic   string
	err     error
	message interface{}
}

type memorySubscriber struct {
	id      string
	topic   string
	exit    chan bool
	handler broker.Handler
	opts    broker.SubscribeOptions
	ctx     context.Context
}

func (m *memoryBroker) Options() broker.Options {
	return m.opts
}

func (m *memoryBroker) Address() string {
	return m.addr
}

func (m *memoryBroker) Connect(ctx context.Context) error {
	m.Lock()
	defer m.Unlock()

	if m.connected {
		return nil
	}

	// use 127.0.0.1 to avoid scan of all network interfaces
	addr, err := maddr.Extract("127.0.0.1")
	if err != nil {
		return err
	}
	i := rand.Intn(20000)
	// set addr with port
	addr = mnet.HostPort(addr, 10000+i)

	m.addr = addr
	m.connected = true

	return nil
}

func (m *memoryBroker) Disconnect(ctx context.Context) error {
	m.Lock()
	defer m.Unlock()

	if !m.connected {
		return nil
	}

	m.connected = false

	return nil
}

func (m *memoryBroker) Init(opts ...broker.Option) error {
	for _, o := range opts {
		o(&m.opts)
	}
	return nil
}

func (m *memoryBroker) Publish(ctx context.Context, topic string, msg *broker.Message, opts ...broker.PublishOption) error {
	m.RLock()
	if !m.connected {
		m.RUnlock()
		return errors.New("not connected")
	}

	subs, ok := m.Subscribers[topic]
	m.RUnlock()
	if !ok {
		return nil
	}

	var v interface{}
	if m.opts.Codec != nil {
		buf, err := m.opts.Codec.Marshal(msg)
		if err != nil {
			return err
		}
		v = buf
	} else {
		v = msg
	}

	p := &memoryEvent{
		topic:   topic,
		message: v,
		opts:    m.opts,
	}

	eh := m.opts.ErrorHandler

	for _, sub := range subs {
		if err := sub.handler(p); err != nil {
			p.err = err
			if sub.opts.ErrorHandler != nil {
				eh = sub.opts.ErrorHandler
			}
			if eh != nil {
				eh(p)
			} else {
				if m.opts.Logger.V(logger.ErrorLevel) {
					m.opts.Logger.Error(err.Error())
				}
			}
			continue
		}
	}

	return nil
}

func (m *memoryBroker) Subscribe(ctx context.Context, topic string, handler broker.Handler, opts ...broker.SubscribeOption) (broker.Subscriber, error) {
	m.RLock()
	if !m.connected {
		m.RUnlock()
		return nil, errors.New("not connected")
	}
	m.RUnlock()

	options := broker.NewSubscribeOptions(opts...)

	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}

	sub := &memorySubscriber{
		exit:    make(chan bool, 1),
		id:      id.String(),
		topic:   topic,
		handler: handler,
		opts:    options,
		ctx:     ctx,
	}

	m.Lock()
	m.Subscribers[topic] = append(m.Subscribers[topic], sub)
	m.Unlock()

	go func() {
		<-sub.exit
		m.Lock()
		var newSubscribers []*memorySubscriber
		for _, sb := range m.Subscribers[topic] {
			if sb.id == sub.id {
				continue
			}
			newSubscribers = append(newSubscribers, sb)
		}
		m.Subscribers[topic] = newSubscribers
		m.Unlock()
	}()

	return sub, nil
}

func (m *memoryBroker) String() string {
	return "memory"
}

func (m *memoryEvent) Topic() string {
	return m.topic
}

func (m *memoryEvent) Message() *broker.Message {
	switch v := m.message.(type) {
	case *broker.Message:
		return v
	case []byte:
		msg := &broker.Message{}
		if err := m.opts.Codec.Unmarshal(v, msg); err != nil {
			if m.opts.Logger.V(logger.ErrorLevel) {
				m.opts.Logger.Error("[memory]: failed to unmarshal: %v", err)
			}
			return nil
		}
		return msg
	}

	return nil
}

func (m *memoryEvent) Ack() error {
	return nil
}

func (m *memoryEvent) Error() error {
	return m.err
}

func (m *memorySubscriber) Options() broker.SubscribeOptions {
	return m.opts
}

func (m *memorySubscriber) Topic() string {
	return m.topic
}

func (m *memorySubscriber) Unsubscribe(ctx context.Context) error {
	m.exit <- true
	return nil
}

func NewBroker(opts ...broker.Option) broker.Broker {
	rand.Seed(time.Now().UnixNano())

	return &memoryBroker{
		opts:        broker.NewOptions(opts...),
		Subscribers: make(map[string][]*memorySubscriber),
	}
}
