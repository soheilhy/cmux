package cmux

import (
	"bytes"
	"io"
	"testing"
)

func TestBuffer(t *testing.T) {
	writeBytes := []byte("deadbeef")

	var b buffer
	for i := 0; i < 10; i++ {
		n, err := b.Write(writeBytes)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(writeBytes) {
			t.Fatalf("cannot write all the bytes: want=%d got=%d", len(writeBytes), n)
		}
	}

	for j := 0; j < 2; j++ {
		readBytes := make([]byte, len(writeBytes))
		for i := 0; i < 10; i++ {
			n, err := b.Read(readBytes)
			if err != nil {
				t.Fatal(err)
			}
			if n != len(readBytes) {
				t.Fatalf("cannot read all the bytes: want=%d got=%d", len(readBytes), n)
			}
			if !bytes.Equal(writeBytes, readBytes) {
				t.Errorf("different bytes read: want=%d got=%d", writeBytes, readBytes)
			}
		}
		_, err := b.Read(readBytes)
		if err != io.EOF {
			t.Errorf("expected EOF")
		}

		b.resetRead()
	}
}

func TestBufferOffset(t *testing.T) {
	writeBytes := []byte("deadbeef")

	var b buffer
	n, err := b.Write(writeBytes)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(writeBytes) {
		t.Fatalf("cannot write all the bytes: want=%d got=%d", len(writeBytes), n)
	}

	for i := 0; i < len(writeBytes)/2; i++ {
		readBytes := make([]byte, 2)
		n, err := b.Read(readBytes)
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatal("cannot read the bytes: want=%d got=%d", 2, n)
		}
		if !bytes.Equal(readBytes, writeBytes[i*2:i*2+2]) {
			t.Fatalf("different bytes read: want=%s got=%s",
				readBytes, writeBytes[i*2:i*2+2])
		}
	}
}
