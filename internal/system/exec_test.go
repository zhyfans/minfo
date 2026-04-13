package system

import "testing"

func TestLineRelayWriterSplitsOnCarriageReturn(t *testing.T) {
	lines := make([]string, 0, 4)
	writer := newLineRelayWriter("stdout", func(stream, line string) {
		lines = append(lines, stream+":"+line)
	})

	if _, err := writer.Write([]byte("Scanning  10% - 00001.M2TS\rScanning  42% - 00002.M2TS\r")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if lines[0] != "stdout:Scanning  10% - 00001.M2TS" {
		t.Fatalf("lines[0] = %q, want first carriage-return line", lines[0])
	}
	if lines[1] != "stdout:Scanning  42% - 00002.M2TS" {
		t.Fatalf("lines[1] = %q, want second carriage-return line", lines[1])
	}
}

func TestLineRelayWriterHandlesCRLFWithoutDuplicateLine(t *testing.T) {
	lines := make([]string, 0, 4)
	writer := newLineRelayWriter("stdout", func(stream, line string) {
		lines = append(lines, line)
	})

	if _, err := writer.Write([]byte("line1\r\nline2\r\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if lines[0] != "line1" || lines[1] != "line2" {
		t.Fatalf("lines = %#v, want [line1 line2]", lines)
	}
}
