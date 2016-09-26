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
//	io.MultiReader(bytes.NewReader(buffer.Bytes()), io.TeeReader(source, buffer))
// Except it does not remove the bytes.Reader from the MultiReader when it hits EOF.
type bufferedReader struct {
	buffer    bytes.Buffer
	snif, src io.Reader
	lastErr   error
}

func newBufferedReader(r io.Reader, snif bool) *bufferedReader {
	s := &bufferedReader{
		src: r,
	}
	
	s.reset(snif)
	
	return s
}

func (s *bufferedReader) Read(p []byte) (n int, err error) {
	if s.buffer.Len() > 0 {
		// If we have already read something from the buffer before, we return the
		// same data and the last error if any. We need to immediately return,
		// otherwise we may block for ever, if we try to be smart and call
		// source.Read() seeking a little bit of more data.
		
		// (puellanivis) This behavior seems really weird, why is read data being
		// returned twice, and not separate readers?
		n, _ = s.buffer.Read(p)
		
	} else {
		// If there is nothing more to return in the sniffed buffer, read from the
		// source.
		n, s.lastErr = s.snif.Read(p)
	}

	return n, s.lastErr
}

func (s *bufferedReader) Reset(snif bool) {
	s.buffer.Reset()

	if !snif {
		s.snif = s.src
	}

	if s.snif == s.src {
		// this is a just in case.
		// if a bufferedReader should never transition from snif==false to snif==true,
		// then this whole if clause can just be removed.
		s.snif = io.TeeReader(s.src, &s.buffer)
	}
}
