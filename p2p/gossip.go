package p2p

import (
	"context"
	"github.com/TIE-Tech/go-logger"
	"reflect"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"google.golang.org/protobuf/proto"
)

const (
	// bufferSize is the size of the queue in go-libp2p-pubsub
	// we should have enough capacity of the queue
	// because there is possibility of that node lose gossip messages when the queue is full
	bufferSize = 512
)

type Topic struct {
	topic   *pubsub.Topic
	typ     reflect.Type
	closeCh chan struct{}
}

func (t *Topic) createObj() proto.Message {
	message, ok := reflect.New(t.typ).Interface().(proto.Message)
	if !ok {
		return nil
	}

	return message
}

func (t *Topic) Publish(obj proto.Message) error {
	data, err := proto.Marshal(obj)
	if err != nil {
		return err
	}

	return t.topic.Publish(context.Background(), data)
}

func (t *Topic) Subscribe(handler func(obj interface{})) error {
	sub, err := t.topic.Subscribe(pubsub.WithBufferSize(bufferSize))
	if err != nil {
		return err
	}

	go t.readLoop(sub, handler)

	return nil
}

func (t *Topic) readLoop(sub *pubsub.Subscription, handler func(obj interface{})) {
	ctx, cancelFn := context.WithCancel(context.Background())

	go func() {
		<-t.closeCh
		cancelFn()
	}()

	for {
		msg, err := sub.Next(ctx)
		if err != nil {
			logger.Error("failed to get topic", "err", err)
			continue
		}

		go func() {
			obj := t.createObj()
			if err := proto.Unmarshal(msg.Data, obj); err != nil {
				logger.Error("failed to unmarshal topic", "err", err)
				return
			}

			handler(obj)
		}()
	}
}

func (s *Server) NewTopic(protoID string, obj proto.Message) (*Topic, error) {
	topic, err := s.ps.Join(protoID)
	if err != nil {
		return nil, err
	}

	tt := &Topic{
		topic: topic,
		typ:   reflect.TypeOf(obj).Elem(),
	}
	return tt, nil
}
