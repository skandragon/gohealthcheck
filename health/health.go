/*
 * Copyright 2022 Michael Graff.
 *
 * Licensed under the Apache License, Version 2.0 (the "License")
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package health

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// Checker is an interface that defines a Check() function.  This Check()
// will be called periorically from a goproc, so if any external resources
// need to be locked, it must handle this correctly.
// It should return an error if the check fails, where the contents of the
// error will be included in the health indicator's JSON.
// Return nil to indicate success.
type Checker interface {
	Check() error
}

type httpChecker struct {
	url        string
	httpClient *http.Client
}

type healthIndicator struct {
	Service     string `json:"service,omitempty"`
	Healthy     bool   `json:"healthy,omitempty"`
	Message     string `json:"message,omitempty"`
	ObserveOnly bool   `json:"observeOnly,omitempty"`
	LastChecked uint64 `json:"lastChecked,omitempty"`

	checker Checker
}

// Health holds state for the current health checker.
type Health struct {
	sync.Mutex
	run        bool
	httpClient *http.Client

	Healthy bool              `json:"healthy,omitempty"`
	Checks  []healthIndicator `json:"checks,omitempty"`
}

// MakeHealth will return a new, empty health checker.
func MakeHealth() *Health {
	return &Health{
		httpClient: http.DefaultClient,
	}
}

func (h *Health) WithHTTPClient(client *http.Client) *Health {
	h.httpClient = client
	return h
}

func removeChecker(s []healthIndicator, i int) []healthIndicator {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

// AddCheck adds a new checker.  For HTTP checkers, use health.HTTPChecker(url).
func (h *Health) AddCheck(service string, observeOnly bool, checker Checker) {
	h.Lock()
	defer h.Unlock()
	for _, c := range h.Checks {
		if c.Service == service {
			c.checker = checker
			c.ObserveOnly = observeOnly
			return
		}
	}
	h.Checks = append(h.Checks, healthIndicator{service, true, "", observeOnly, 0, checker})
}

// RemoveCheck removes a checker.  This will eventually converge in the output.
func (h *Health) RemoveCheck(service string) {
	h.Lock()
	defer h.Unlock()
	for idx, c := range h.Checks {
		if c.Service == service {
			h.Checks = removeChecker(h.Checks, idx)
			return
		}
	}
}

// This is called while (h) is unlocked.
func (h *Health) runChecker(checker *healthIndicator) {
	err := checker.checker.Check()
	if err == nil {
		checker.Healthy = true
		checker.Message = "OK"
	} else {
		checker.Healthy = false
		checker.Message = fmt.Sprintf("%s ERROR %v", checker.Service, err)
	}
	checker.LastChecked = uint64(time.Now().UnixMilli())
}

// RunCheckers runs all the health checks, one every frequency/count seconds.
func (h *Health) RunCheckers(frequency int) {
	nextIndex := 0
	firstPass := true // used to ensure we scan fast on first start

	h.Lock()
	h.run = true
	count := len(h.Checks) + 1 // ensure we are at least 1
	h.Unlock()

	for {
		// ensure we sleep while not locked.
		sleepDuration := time.Duration(frequency) * time.Second / time.Duration(count)
		if firstPass {
			sleepDuration = time.Duration(10) * time.Millisecond
		}
		time.Sleep(sleepDuration)

		// locked while manitulating things and calling healthcheck
		h.Lock()
		count = len(h.Checks) + 1
		if !h.run {
			h.Unlock()
			return
		}
		if nextIndex >= len(h.Checks) {
			nextIndex = 0
			firstPass = false
		}
		if len(h.Checks) > 0 {
			h.Unlock()
			h.runChecker(&h.Checks[nextIndex])
			h.Lock()
		}
		nextIndex++

		// Now, check all statuses and compute the global status
		h.Healthy = true
		for _, c := range h.Checks {
			if c.ObserveOnly {
				continue
			}
			h.Healthy = h.Healthy && c.Healthy
		}
		h.Unlock()
	}
}

// StopCheckers will stop running RunCheckers()
func (h *Health) StopCheckers() {
	h.Lock()
	defer h.Unlock()
	h.run = false
}

// HTTPChecker adds returns a HealthChecker that will
// poll the provided URL, and use any http error
// or status code to indicate success or failure.
func (h *Health) HTTPChecker(url string) Checker {
	return &httpChecker{url: url, httpClient: h.httpClient}
}

// HTTPHandler which returns 200 if all critical checks pass, or 500 if not.
func (h *Health) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		h.Lock()
		data, err := json.Marshal(h)
		healthy := h.Healthy
		h.Unlock()
		if err != nil {
			w.WriteHeader(500)
			log.Printf("Healthcheck HTTPHandler: %v", err)
			return
		}
		if healthy {
			w.WriteHeader(200)
		} else {
			w.WriteHeader(418)
		}
		written, err := w.Write(data)
		if err != nil {
			log.Printf("when writing body: %v", err)
		}
		if written != len(data) {
			log.Printf("unable to write entire body, %d of %d bytes", written, len(data))
		}
	}
}

// Check implements the HealthChecker interface, using a HTTP fetch.
// Any status code between 200 and 399 indicates success, any other
// indicates a failure.
func (hc *httpChecker) Check() error {
	client := hc.httpClient
	resp, err := client.Get(hc.url)
	if err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	return fmt.Errorf("HTTP status code %d returned", resp.StatusCode)
}
