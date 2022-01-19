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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testChecker struct {
	called bool
	err    error
}

func (tc *testChecker) Check() error {
	tc.called = true
	return tc.err
}

var failChecker = testChecker{err: fmt.Errorf("Generic Error")}
var successChecker = testChecker{}

func resetCheckers() {
	failChecker.called = false
	successChecker.called = false
}

func Test_Health_callsChecker(t *testing.T) {
	h := MakeHealth()
	resetCheckers()

	h.AddCheck("test", false, &successChecker)
	h.runChecker(&h.Checks[0])
	assert.True(t, successChecker.called)
	assert.True(t, h.Checks[0].Healthy)
	assert.Equal(t, "OK", h.Checks[0].Message)
}
