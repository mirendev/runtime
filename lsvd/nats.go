package lsvd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/nats-io/nats.go"
)

type NATSConnector struct {
	log  *slog.Logger
	d    *Disk
	id   string
	conn *nats.Conn

	controlSub *nats.Subscription
}

func NewNATSConnector(log *slog.Logger, d *Disk, url, id string) (*NATSConnector, error) {
	conn, err := nats.Connect(url)
	if err != nil {
		return nil, err
	}

	nc := &NATSConnector{
		log:  log,
		d:    d,
		id:   id,
		conn: conn,
	}

	return nc, nil
}

func (n *NATSConnector) Start(ctx context.Context) error {
	go n.startPeriodic(ctx, 1*time.Minute)
	n.startControllerInput(ctx)

	return nil
}

func (n *NATSConnector) startPeriodic(ctx context.Context, dur time.Duration) {
	ticker := time.NewTicker(dur)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := n.publishStats()
			if err != nil {
				n.log.Error("error publishing periodic stats", "error", err)
			}
		}
	}
}

func (n *NATSConnector) subj(which string) string {
	return fmt.Sprintf("lsvd.disk.%s.%s", n.id, which)
}

func (n *NATSConnector) publishStats() error {
	data, err := json.Marshal(&StatsMessage{
		Id:          n.id,
		PublishTime: time.Now(),
		GCCycles:    counterValue(gcCount),
		GCTime:      counterValueFloat(gcTime),
		Extents:     counterValue(extents),
		Density:     counterValueFloat(dataDensity),
	})

	if err != nil {
		return err
	}

	return n.conn.Publish(n.subj("stats"), data)
}

func (n *NATSConnector) publish(subject string, value any) error {
	data, err := cbor.Marshal(value)
	if err != nil {
		return err
	}

	return n.conn.Publish(subject, data)
}

type StatsMessage struct {
	Id          string    `json:"id" cbor:"10,keyasint"`
	PublishTime time.Time `json:"published_at" cbor:"1,keyasint"`
	GCCycles    int64     `json:"gc_cycles" cbor:"2,keyasint"`
	GCTime      float64   `json:"gc_time" cbor:"3,keyasint"`
	Extents     int64     `json:"extents" cbor:"4,keyasint"`
	Density     float64   `json:"density" cbor:"5,keyasint"`
}

type ControlMessage struct {
	Kind      string `json:"kind"`
	SegmentId string `json:"segment,omitempty"`
}

func (n *NATSConnector) startControllerInput(ctx context.Context) error {
	sub, err := n.conn.Subscribe(n.subj("control"), func(msg *nats.Msg) {
		var cm ControlMessage

		err := json.Unmarshal(msg.Data, &cm)
		if err != nil {
			n.log.Error("error decoding control message", "error", err)
			return
		}

		n.log.Debug("recieved control message via NATS", "kind", cm.Kind)

		switch cm.Kind {
		case "close-segment":
			seg, err := ParseSegment(cm.SegmentId)
			if err != nil {
				n.log.Error("invalid segment id", "id", cm.SegmentId)
			}

			select {
			case <-ctx.Done():
				return
			case n.d.controller.EventsCh() <- Event{
				Kind:      CloseSegment,
				SegmentId: seg,
			}:
				// ok
			}

		case "sweep-small-segments":
			select {
			case <-ctx.Done():
				return
			case n.d.controller.EventsCh() <- Event{
				Kind: SweepSmallSegments,
			}:
				// ok
			}

		case "improve-density":
			select {
			case <-ctx.Done():
				return
			case n.d.controller.EventsCh() <- Event{
				Kind: ImproveDensity,
			}:
				// ok
			}
		}
	})

	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		sub.Unsubscribe()
	}()

	n.controlSub = sub

	return nil
}
