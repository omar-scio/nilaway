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
	S1 *S
	S2 *S
	S3 *S
	S4 *S
	S5 *S
}

func (s *SSuite) SetupTest1() {
	var err error
	s.S1, err = NewS()
	s.NoError(err)
	print(s.S1.f) // safe
}

func (s *SSuite) Test1() {
	print(s.S1.f) // safe
}

func (s *SSuite) SetupTest2() {
	var err error
	s.S2, err = NewS()
	print(s.S2.f) //want "lacking guarding"
	s.Nil(err)
}

func (s *SSuite) Test2() {
	print(s.S2.f) // safe
}

func (s *SSuite) SetupTest3() {
	s.S3, _ = NewS()
	print(s.S3.f) //want "result 0 of `NewS.*` lacking guarding"
}

func (s *SSuite) Test3() {
	print(s.S3.f) //want "result 0 of `NewS.*` lacking guarding"
}

func (s *SSuite) SetupTest4() {
	temp, err := NewS()
	s.NoError(err)
	s.S4 = temp
	print(s.S4.f) // safe
}

func (s *SSuite) Test4() {
	print(s.S4.f) // safe
}

func (s *SSuite) SetupTest5() {
	var err1, err2 error
	s.S5, err1 = NewS()
	s.NoError(err2)
	_ = err1
	print(s.S5.f) //want "result 0 of `NewS.*` lacking guarding"
}

func (s *SSuite) Test5() {
	print(s.S5.f) // unsafe
}

// Below test checks for field assignment via a map lookup.
type T struct {
	suite.Suite
	f1 *int
	f2 *int
}

func (t *T) SetupTest1(mp map[int]*int, i int) {
	var ok bool
	t.f1, ok = mp[0]
	t.True(ok)
	print(*t.f1) // safe

	t.f2, ok = mp[i]
	print(*t.f2) //want "deep read from parameter `mp` lacking guarding"
	t.True(ok)
	print(*t.f2)
}

func (t *T) Test1() {
	print(*t.f1) // safe
	print(*t.f2) // safe
}

// type M struct {
// 	suite.Suite
// 	mp map[int]*string
// }
//
// func (m *M) SetupTest2(localMap map[string]*string) {
// 	var ok bool
// 	m.mp[0], ok = localMap["abc"]
// 	m.True(ok)
// 	print(*m.mp[0]) // safe
// }
//
// func (m *M) TestField2() {
// 	print(*m.mp[0]) // safe
// }
