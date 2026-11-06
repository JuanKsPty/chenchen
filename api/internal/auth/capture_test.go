package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestACaptureKeyIsNoOtherToken(t *testing.T) {
	key, hash, err := NewCaptureKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, CaptureKeyPrefix) || len(key) != captureKeyLen || !looksLikeCaptureKey(key) {
		t.Errorf("NewCaptureKey = %q, not shaped like a capture key", key)
	}
	// The hash is of the whole key: the same token without its prefix, or
	// taken for a device token, is a different credential.
	if hash != HashCaptureKey(key) || hash == HashCaptureKey(strings.TrimPrefix(key, CaptureKeyPrefix)) ||
		strings.Contains(hash, strings.TrimPrefix(key, CaptureKeyPrefix)) {
		t.Errorf("HashCaptureKey(%q) = %q", key, hash)
	}
	again, _, err := NewCaptureKey()
	if err != nil || again == key {
		t.Errorf("two keys are the same: %q", key)
	}

	device, _, err := NewDeviceToken()
	if err != nil {
		t.Fatal(err)
	}
	if looksLikeCaptureKey(device) || looksLikeCaptureKey(CaptureKeyPrefix+device[:42]) ||
		looksLikeCaptureKey(CaptureKeyPrefix+device[:42]+"=") || looksLikeCaptureKey(" "+key) {
		t.Error("a device token, or a key cut short or padded, passes for a capture key")
	}
}

func TestAuthenticateTurnsAwayWhatIsNoKeyWithoutALookup(t *testing.T) {
	// No database at all: reaching one would panic, so passing proves every
	// one of these is refused by its shape alone.
	keys := &CaptureKeys{}
	jwt, err := newTestIssuer(t).IssueAccessToken(uuid.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := NewCaptureKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{
		"",
		"Bearer",
		"Bearer " + jwt,
		jwt,
		"Basic " + key,
		"Bearer " + strings.ToUpper(key),
		"Bearer " + key + "x",
		"Bearer " + key[:len(key)-1] + "!",
		"Bearer chn_",
	} {
		if _, err := keys.Authenticate(context.Background(), header); !errors.Is(err, ErrCaptureKeyInvalid) {
			t.Errorf("Authenticate(%q) = %v, want ErrCaptureKeyInvalid", header, err)
		}
	}
}
