package cmux

import (
	"bytes"
	"io"
	"testing"
)

func TestWriteNoModify(t *testing.T) {
	var b buffer

	const origWriteByte = 0
	const postWriteByte = 1

	writeBytes := []byte{origWriteByte}
	if _, err := b.Write(writeBytes); err != nil {
		t.Fatal(err)
	}
	writeBytes[0] = postWriteByte
	readBytes := make([]byte, 1)
	if _, err := b.Read(readBytes); err != io.EOF {
		t.Fatal(err)
	}

	if readBytes[0] != origWriteByte {
		t.Fatalf("expected to read %x, but read %x; buffer retained passed-in slice", origWriteByte, postWriteByte)
	}
}

const writeString = "deadbeef"

func TestBuffer(t *testing.T) {
	writeBytes := []byte(writeString)

	const numWrites = 10

	var b buffer
	for i := 0; i < numWrites; i++ {
		n, err := b.Write(writeBytes)
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
		if n != len(writeBytes) {
			t.Fatalf("cannot write all the bytes: want=%d got=%d", len(writeBytes), n)
		}
	}

	for j := 0; j < 2; j++ {
		readBytes := make([]byte, len(writeBytes))
		for i := 0; i < numWrites; i++ {
			n, err := b.Read(readBytes)
			if i == numWrites-1 {
				// The last read should report EOF.
				if err != io.EOF {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if n != len(readBytes) {
				t.Fatalf("cannot read all the bytes: want=%d got=%d", len(readBytes), n)
			}
			if !bytes.Equal(writeBytes, readBytes) {
				t.Errorf("different bytes read: want=%d got=%d", writeBytes, readBytes)
			}
		}
		n, err := b.Read(readBytes)
		if err != io.EOF {
			t.Errorf("expected EOF")
		}
		if n != 0 {
			t.Errorf("expected buffer to be empty, but got %d bytes", n)
		}

		b.resetRead()
	}
}

func TestBufferOffset(t *testing.T) {
	writeBytes := []byte(writeString)

	var b buffer
	n, err := b.Write(writeBytes)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(writeBytes) {
		t.Fatalf("cannot write all the bytes: want=%d got=%d", len(writeBytes), n)
	}

	const readSize = 2

	numReads := len(writeBytes) / readSize

	for i := 0; i < numReads; i++ {
		readBytes := make([]byte, readSize)
		n, err := b.Read(readBytes)
		if i == numReads-1 {
			// The last read should report EOF.
			if err != io.EOF {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if n != readSize {
			t.Fatalf("cannot read the bytes: want=%d got=%d", readSize, n)
		}
		if got := writeBytes[i*readSize : i*readSize+readSize]; !bytes.Equal(got, readBytes) {
			t.Fatalf("different bytes read: want=%s got=%s", readBytes, got)
		}
	}
}
