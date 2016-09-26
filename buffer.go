// Copyright 2016 The CMux Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package cmux

import (
	"bytes"
	"io"
)

// bufferedReader is an optimized implementation of io.Reader that behaves like
// ```
// io.MultiReader(bytes.NewReader(buffer.Bytes()), io.TeeReader(source, buffer))
// ```
// without allocating.
type bufferedReader struct {
	source  io.Reader
	src     io.Reader

	buf	bytes.Buffer

	rbuf	[]byte
	off     int
}

func (s *bufferedReader) Read(p []byte) (int, error) {
	if len(s.rbuf) > 0 {
		if s.off < len(s.rbuf) {
			n := copy(p, s.rbuf[s.off:])
			s.off += n
			return n, nil
		}

		// we will need to reset to s.buf.Bytes() anyways.
		s.rbuf = nil
	}

	return s.src.Read(p)
}


// reset starts over at the begining of the buffer again, and if we're sniffing
// then we will use an io.TeeReader, and if we're not sniffing anymore, then
// we just use the source itself, and reset the internal buffer.
// Basically, this starts the whole read over again from the recorded beginning.
func (s *bufferedReader) reset(snif bool) {
	// use a direct []byte array because there's some overhead in bytes.Buffer
	// to support unreading, and other things we don't need or want.
	// benchmarking suggested it was statistically significant overhead.
	s.off = 0
	s.rbuf = s.buf.Bytes()

	if !snif {
		s.src = s.source
		s.buf.Reset()
		return
	}

	if s.src == nil {
		// we do use bytes.Buffer.Write though, because it's very optimized.
		s.src = io.TeeReader(s.source, &s.buf)
	}
}
