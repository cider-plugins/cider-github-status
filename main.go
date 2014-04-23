// Copyright (c) 2014 The AUTHORS
//
// This file is part of paprika-github-status.
//
// paprika-github-status is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// paprika-github-status is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with paprika-github-status.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	// Stdlib
	"os"
	"os/signal"
	"strings"
	"syscall"

	// Cider
	"github.com/cider/go-cider/cider/services/logging"
	"github.com/cider/go-cider/cider/services/pubsub"
	zlogging "github.com/cider/go-cider/cider/transports/zmq3/logging"
	zpubsub "github.com/cider/go-cider/cider/transports/zmq3/pubsub"

	// Others
	"code.google.com/p/goauth2/oauth"
	"github.com/google/go-github/github"
	zmq "github.com/pebbe/zmq3"
)

func main() {
	// Initialise the Logging service.
	logger, err := logging.NewService(func() (logging.Transport, error) {
		factory := zlogging.NewTransportFactory()
		factory.MustReadConfigFromEnv("CIDER_ZMQ3_LOGGING_").MustBeFullyConfigured()
		return factory.NewTransport(os.Getenv("CIDER_ALIAS"))
	})
	if err != nil {
		panic(err)
	}
	defer zmq.Term()
	defer logger.Close()

	if err := innerMain(logger); err != nil {
		panic(err)
	}
}

func innerMain(logger *logging.Service) error {
	// Read the required variables from the environment.
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return logger.Critical("GITHUB_TOKEN is not set")
	}

	// Initialise the GitHub client.
	transport := oauth.Transport{
		Token: &oauth.Token{AccessToken: token},
	}
	client := github.NewClient(transport.Client())

	// Initialise the PubSub service.
	eventBus, err := pubsub.NewService(func() (pubsub.Transport, error) {
		factory := zpubsub.NewTransportFactory()
		factory.MustReadConfigFromEnv("CIDER_ZMQ3_PUBSUB_").MustBeFullyConfigured()
		return factory.NewTransport(os.Getenv("CIDER_ALIAS"))
	})
	if err != nil {
		return logger.Critical(err)
	}
	defer func() {
		select {
		case <-eventBus.Closed():
			goto Wait
		default:
		}
		if err := eventBus.Close(); err != nil {
			logger.Critical(err)
		}
	Wait:
		if err := eventBus.Wait(); err != nil {
			logger.Critical(err)
		}
	}()

	// Start catching signals.
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	// Subscribe to all significant build events.
	if _, err := eventBus.Subscribe("paprika.build.enqueued", func(event pubsub.Event) {
		// Unmarshal the event object.
		var body BuildEnqueuedEvent
		if err := event.Unmarshal(&body); err != nil {
			logger.Warn(err)
			return
		}

		// Do nothing if the build source is not a pull request.
		if body.PullRequest == nil {
			logger.Info("ENQUEUED event received, but not a pull request, skipping...")
			return
		}

		// Set the pull request status to pending.
		logger.Infof("Setting status for %v to PENDING", body.PullRequest.HTMLURL)
		if err := postStatus(client, body.PullRequest.StatusesURL, &github.RepoStatus{
			State:       github.String("pending"),
			Description: github.String("The build is enqueued or running"),
			Context:     github.String("Paprika CI"),
		}); err != nil {
			logger.Error(err)
			return
		}
	}); err != nil {
		return logger.Critical(err)
	}

	if _, err := eventBus.Subscribe("paprika.build.finished", func(event pubsub.Event) {
		// Unmarshal the event object.
		var body BuildFinishedEvent
		if err := event.Unmarshal(&body); err != nil {
			logger.Warn(err)
			return
		}

		// Do nothing if the build source is not a pull request.
		if body.PullRequest == nil {
			logger.Info("FINISHED event received, but not a pull request, skipping...")
			return
		}

		// Set the final pull request status.
		var desc string
		switch body.Result {
		case "success":
			desc = "The build succeeded!"
		case "failure":
			desc = "The build failed!"
		case "error":
			if body.Error != "" {
				desc = body.Error
			} else {
				desc = "Paprika exploded, oops"
			}
		}

		logger.Infof("Setting status for %v to %v",
			body.PullRequest.HTMLURL, strings.ToUpper(body.Result))
		if err := postStatus(client, body.PullRequest.StatusesURL, &github.RepoStatus{
			State:       &body.Result,
			TargetURL:   body.OutputURL,
			Description: &desc,
			Context:     github.String("Paprika CI"),
		}); err != nil {
			logger.Error(err)
			return
		}
	}); err != nil {
		return logger.Critical(err)
	}

	// Start processing signals, block until crashed or terminated.
	select {
	case <-eventBus.Closed():
		return eventBus.Wait()
	case <-signalCh:
		return nil
	}
}

func postStatus(client *github.Client, statusesURL string, status *github.RepoStatus) error {
	req, err := client.NewRequest("POST", statusesURL, status)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/vnd.github.she-hulk-preview+json")

	_, err = client.Do(req, nil)
	return err
}
