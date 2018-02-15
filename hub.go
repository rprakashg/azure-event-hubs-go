package eventhub

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"pack.ag/amqp"
	"path"
	"sync"
)

const (
	maxUserAgentLen = 128
	rootUserAgent   = "/golang-event-hubs"
)

type (
	hub struct {
		name              string
		namespace         *Namespace
		receivers         []*receiver
		sender            *sender
		senderPartitionID *string
		receiverMu        sync.Mutex
		senderMu          sync.Mutex
		offsetPersister   OffsetPersister
		userAgent         string
	}

	// Handler is the function signature for any receiver of AMQP messages
	Handler func(context.Context, *amqp.Message) error

	// Sender provides the ability to send a messages
	Sender interface {
		Send(ctx context.Context, message *amqp.Message, opts ...SendOption) error
		//SendBatch(ctx context.Context, mesages []*amqp.Message, opts ...SendOption) error
	}

	// Receiver provides the ability to receive messages
	Receiver interface {
		Receive(partitionID string, handler Handler, opts ...ReceiveOption) error
	}

	// Closer provides the ability to close a connection or client
	Closer interface {
		Close() error
	}

	// SenderReceiver provides the ability to send and receive AMQP messages
	SenderReceiver interface {
		Sender
		Receiver
		Closer
	}

	// HubOption provides structure for configuring new Event Hub instances
	HubOption func(h *hub) error

	// OffsetPersister provides persistence for the received offset for a given namespace, hub name, consumer group, partition Id and
	// offset so that if a receiver where to be interrupted, it could resume after the last consumed event.
	OffsetPersister interface {
		Write(namespace, name, consumerGroup, partitionID, offset string) error
		Read(namespace, name, consumerGroup, partitionID string) (string, error)
	}
)

// Close drains and closes all of the existing senders, receivers and connections
func (h *hub) Close() error {
	for _, r := range h.receivers {
		r.Close()
	}
	return nil
}

// Listen subscribes for messages sent to the provided entityPath.
func (h *hub) Receive(partitionID string, handler Handler, opts ...ReceiveOption) error {
	h.receiverMu.Lock()
	defer h.receiverMu.Unlock()

	receiver, err := h.newReceiver(partitionID, opts...)
	if err != nil {
		return err
	}

	h.receivers = append(h.receivers, receiver)
	receiver.Listen(handler)
	return nil
}

// Send sends an AMQP message to the broker
func (h *hub) Send(ctx context.Context, message *amqp.Message, opts ...SendOption) error {
	sender, err := h.getSender()
	if err != nil {
		return err
	}
	return sender.Send(ctx, message, opts...)
}

// Send sends a batch of AMQP message to the broker
func (h *hub) SendBatch(ctx context.Context, messages []*amqp.Message, opts ...SendOption) error {
	return fmt.Errorf("not implemented")
}

// HubWithPartitionedSender configures the hub instance to send to a specific event hub partition
func HubWithPartitionedSender(partitionID string) HubOption {
	return func(h *hub) error {
		h.senderPartitionID = &partitionID
		return nil
	}
}

// HubWithOffsetPersistence configures the hub instance to read and write offsets so that if a hub is interrupted, it
// can resume after the last consumed event.
func HubWithOffsetPersistence(offsetPersister OffsetPersister) HubOption {
	return func(h *hub) error {
		h.offsetPersister = offsetPersister
		return nil
	}
}

// HubWithUserAgent configures the hub to append the given string to the user agent sent to the server
//
// This option can be specified multiple times to add additional segments.
//
// Max user agent length is specified by the const maxUserAgentLen.
func HubWithUserAgent(userAgent string) HubOption {
	return func(h *hub) error {
		return h.appendAgent(userAgent)
	}
}

func (h *hub) appendAgent(userAgent string) error {
	ua := path.Join(h.userAgent, userAgent)
	if len(ua) > maxUserAgentLen {
		return errors.Errorf("user agent string has surpassed the max length of %d", maxUserAgentLen)
	}
	h.userAgent = ua
	return nil
}

func (h *hub) getSender() (*sender, error) {
	h.senderMu.Lock()
	defer h.senderMu.Unlock()

	if h.sender == nil {
		s, err := h.newSender()
		if err != nil {
			return nil, err
		}
		h.sender = s
	}
	// add recover logic here
	return h.sender, nil
}