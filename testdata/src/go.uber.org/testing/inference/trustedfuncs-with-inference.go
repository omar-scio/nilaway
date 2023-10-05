//  Copyright (c) 2023 Uber Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This package aims to test behavior specific to field assigns via error returning functions and map lookups in unit tests
// that use the `github.com/stretchr/testify` library

package inference

import "go.uber.org/testing/github.com/stretchr/testify/suite"

// Below test checks for field assignment via an error returning function.
type S struct {
	f *int
}

var dummy bool

type myErr struct{}

func (myErr) Error() string { return "myErr message" }

func NewS() (*S, error) {
	if dummy {
		return &S{}, nil
	}
	return nil, myErr{}
}

type SSuite struct {
	suite.Suite
	S *S
}

func (s *SSuite) testErrorRetFunction(i int) {
	var err error

	switch i {
	case 0:
		s.S, err = NewS()
		s.NoError(err)
		print(s.S.f) // safe

	case 1:
		s.S, err = NewS()
		print(s.S.f) //want "lacking guarding"
		s.Nil(err)
		print(s.S.f) // safe

	case 2:
		s.S, _ = NewS()
		print(s.S.f) //want "result 0 of `NewS.*` lacking guarding"

	case 3:
		temp, err := NewS()
		s.NoError(err)
		s.S = temp
		print(s.S.f) // safe

	case 4:
		var err1, err2 error
		s.S, err1 = NewS()
		s.NoError(err2)
		_ = err1
		print(s.S.f) //want "result 0 of `NewS.*` lacking guarding"
	}
}

// Below test checks for field assignment via map lookup.
type T struct {
	suite.Suite
	f1 *int
	f2 *int
}

func (t *T) testMapRead(mp map[int]*int, i int) {
	var ok bool
	t.f1, ok = mp[0]
	t.True(ok)
	print(*t.f1) // safe

	t.f2, ok = mp[i]
	print(*t.f2) //want "deep read from parameter `mp` lacking guarding"
	t.True(ok)
	print(*t.f2)
}

type M struct {
	suite.Suite
	mp map[int]*int
}

func (m *M) testFieldMapAssign(mp map[int]*int, i int) {
	var ok bool
	m.mp[0], ok = mp[0]
	m.True(ok)
	print(*m.mp[0]) // safe

	// `mp[i]` is not considered to be a stable expression, hence an error would be reported despite the ok check.
	m.mp[i], ok = mp[0]
	m.True(ok)
	print(*m.mp[i]) //want "deep read from field `mp` lacking guarding"
}

// Below test checks for field assignment via channel receive.
type C struct {
	suite.Suite
	f *int
}

func (c *C) testChannelRecv(ch chan *int) {
	var ok bool
	c.f, ok = <-ch

	c.False(ok)
	print(*c.f) //want "deep read from parameter `ch` lacking guarding"

	c.True(ok)
	print(*c.f) // safe
}
