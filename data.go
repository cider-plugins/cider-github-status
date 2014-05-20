// Copyright (c) 2013-2014 The cider-github-status AUTHORS
//
// Use of this source code is governed by the MIT license
// that can be found in the LICENSE file.

package main

type BuildEnqueuedEvent struct {
	PullRequest *PullRequest `codec:"pull_request,omitempty"`
}

type BuildFinishedEvent struct {
	Result      string       `codec:"result"`
	PullRequest *PullRequest `codec:"pull_request,omitempty"`
	OutputURL   *string      `codec:"output_url,omitempty"`
	Error       string       `codec:"error,omitempty"`
}

type PullRequest struct {
	HTMLURL     string `codec:"html_url"`
	StatusesURL string `codec:"statuses_url"`
}
