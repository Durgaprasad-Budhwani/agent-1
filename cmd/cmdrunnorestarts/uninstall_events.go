package cmdrunnorestarts

import (
	"context"
	"fmt"

	"github.com/pinpt/go-common/datamodel"
	"github.com/pinpt/go-common/event/action"
	"github.com/pinpt/integration-sdk/agent"
)

func (s *runner) handleUninstallEvents(ctx context.Context, finishMain chan bool) (closefunc, error) {
	s.logger.Info("listening for uninstall requests")

	errorsChan := make(chan error, 1)

	actionConfig := action.Config{
		APIKey:  s.conf.APIKey,
		GroupID: fmt.Sprintf("agent-%v", s.conf.DeviceID),
		Channel: s.conf.Channel,
		Factory: factory,
		Topic:   agent.UninstallRequestModelName.String(),
		Errors:  errorsChan,
		Headers: map[string]string{
			"customer_id": s.conf.CustomerID,
			"uuid":        s.conf.DeviceID,
		},
	}

	cb := func(instance datamodel.ModelReceiveEvent) (datamodel.ModelSendEvent, error) {

		s.logger.Info("received uninstall request")

		defer func() { finishMain <- true }()
		return nil, nil
	}

	go func() {
		for err := range errorsChan {
			s.logger.Error("error in integration requests", "err", err)
		}
	}()

	sub, err := action.Register(ctx, action.NewAction(cb), actionConfig)
	if err != nil {
		return nil, err
	}

	sub.WaitForReady()

	return func() { sub.Close() }, nil
}
