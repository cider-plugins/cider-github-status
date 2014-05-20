// Copyright (c) 2013-2014 The cider-github-status AUTHORS
//
// Use of this source code is governed by the MIT license
// that can be found in the LICENSE file.

package main

import (
	// Stdlib
	"os"
	"os/signal"
	"strings"
	"syscall"

	// Meeko
	"github.com/meeko/go-meeko/agent"
	"github.com/meeko/go-meeko/meeko/services/pubsub"

	// Others
	"code.google.com/p/goauth2/oauth"
	"github.com/google/go-github/github"
)

func main() {
	// Make sure the Meeko agent is terminated properly.
	defer agent.Terminate()

	// Run the main function.
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	// Some userful shortcuts.
	var (
		log      = agent.Logging
		eventBus = agent.PubSub
	)

	// Read the required variables from the environment.
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return log.Critical("GITHUB_TOKEN is not set")
	}

	// Initialise the GitHub client.
	t := oauth.Transport{
		Token: &oauth.Token{AccessToken: token},
	}
	gh := github.NewClient(t.Client())

	// Start catching signals.
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	// Subscribe to all significant build events.
	if _, err := eventBus.Subscribe("cider.build.enqueued", func(event pubsub.Event) {
		// Unmarshal the event object.
		var body BuildEnqueuedEvent
		if err := event.Unmarshal(&body); err != nil {
			log.Warn(err)
			return
		}

		// Do nothing if the build source is not a pull request.
		if body.PullRequest == nil {
			log.Info("ENQUEUED event received, but not a pull request, skipping...")
			return
		}

		// Set the pull request status to pending.
		log.Infof("Setting status for %v to PENDING", body.PullRequest.HTMLURL)
		if err := postStatus(gh, body.PullRequest.StatusesURL, &github.RepoStatus{
			State:       github.String("pending"),
			Description: github.String("The build is enqueued or running"),
			Context:     github.String("Cider CI"),
		}); err != nil {
			log.Error(err)
			return
		}
	}); err != nil {
		return log.Critical(err)
	}

	if _, err := eventBus.Subscribe("cider.build.finished", func(event pubsub.Event) {
		// Unmarshal the event object.
		var body BuildFinishedEvent
		if err := event.Unmarshal(&body); err != nil {
			log.Warn(err)
			return
		}

		// Do nothing if the build source is not a pull request.
		if body.PullRequest == nil {
			log.Info("FINISHED event received, but not a pull request, skipping...")
			return
		}

		// Set the final pull request status.
		var desc string
		switch body.Result {
		case "success":
			desc = "Cider build succeeded!"
		case "failure":
			desc = "Cider build failed!"
		case "error":
			if body.Error != "" {
				desc = body.Error
			} else {
				desc = "Cider exploded, oops"
			}
		}

		log.Infof("Setting status for %v to %v",
			body.PullRequest.HTMLURL, strings.ToUpper(body.Result))
		if err := postStatus(gh, body.PullRequest.StatusesURL, &github.RepoStatus{
			State:       &body.Result,
			TargetURL:   body.OutputURL,
			Description: &desc,
			Context:     github.String("Cider"),
		}); err != nil {
			log.Error(err)
			return
		}
	}); err != nil {
		return log.Critical(err)
	}

	// Start processing signals, block until crashed or terminated.
	select {
	case <-signalCh:
		log.Info("Signal received, exiting...")
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
