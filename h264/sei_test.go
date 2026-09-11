package h264

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/jurry/seimark/marker"
)

// The worked example from docs/format.md.
const (
	exampleBodyHex = "01 00 00 06 5b 3b 5e 16 94 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51"
	exampleNALHex  = "06 05 26 44 a7 3c b9 b3 6c 45 9a 8f 1a a3 aa 43 1f 62 4a 01 00 00 06 5b 3b 5e 16 94 00 00 03 00 00 03 00 9f 3c 1a 77 e2 b0 4d 51 80"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func TestUserDataSEINALMatchesSpecExample(t *testing.T) {
	got, err := UserDataSEINAL(marker.FormatUUID, unhex(t, exampleBodyHex))
	if err != nil {
		t.Fatal(err)
	}
	if want := unhex(t, exampleNALHex); !bytes.Equal(got, want) {
		t.Fatalf("got  %x\nwant %x", got, want)
	}
}

func TestUserDataSEINALLargeBodyUsesMultiBytePayloadSize(t *testing.T) {
	body := bytes.Repeat([]byte{0x11}, 300) // payloadSize 316 = 0xff + 0x3d
	got, err := UserDataSEINAL(marker.FormatUUID, body)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 0x06 || got[1] != 0x05 || got[2] != 0xff || got[3] != 0x3d {
		t.Fatalf("header bytes %x, want 06 05 ff 3d", got[:4])
	}
	if got[len(got)-1] != 0x80 {
		t.Fatalf("last byte %x, want 80", got[len(got)-1])
	}
}
