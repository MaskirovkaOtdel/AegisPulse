package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMissingSignature    = errors.New("missing signature header")
	ErrInvalidHeaderFormat = errors.New("invalid signature header format; expected t={timestamp},v1={hex}")
	ErrExpiredTimestamp    = errors.New("signature timestamp has expired (anti-replay defense)")
	ErrClockDrift          = errors.New("signature timestamp is too far in future (clock drift)")
	ErrSignatureMismatch   = errors.New("signature mismatch; constant-time validation failed")
)

// GenerateSignature signs the payload with secret and timestamp.
// Format: t={timestamp},v1={hex(HMAC-SHA256(secret, timestamp + "." + payload))}
func GenerateSignature(payload []byte, secret []byte, timestamp int64) string {
	mac := hmac.New(sha256.New, secret)
	prefix := fmt.Sprintf("%d.", timestamp)
	mac.Write([]byte(prefix))
	mac.Write(payload)
	sigHex := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, sigHex)
}

// VerifySignature validates a webhook signature against tolerance window and secret using constant-time comparison
func VerifySignature(rawBody []byte, signatureHeader string, secret []byte, toleranceSeconds int64, nowUnix int64) (bool, error) {
	if signatureHeader == "" {
		return false, ErrMissingSignature
	}

	parts := strings.Split(signatureHeader, ",")
	var timestampStr, v1Hex string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "t=") {
			timestampStr = strings.TrimPrefix(part, "t=")
		} else if strings.HasPrefix(part, "v1=") {
			v1Hex = strings.TrimPrefix(part, "v1=")
		}
	}

	if timestampStr == "" || v1Hex == "" {
		return false, ErrInvalidHeaderFormat
	}

	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return false, ErrInvalidHeaderFormat
	}

	if nowUnix == 0 {
		nowUnix = time.Now().Unix()
	}

	// Anti-replay expiration check: timestamp is older than allowed window
	if (nowUnix - ts) > toleranceSeconds {
		return false, fmt.Errorf("%w: age=%ds, tolerance=%ds", ErrExpiredTimestamp, nowUnix-ts, toleranceSeconds)
	}

	// Clock drift check: timestamp is in the future beyond a 10s tolerance
	if (ts - nowUnix) > 10 {
		return false, fmt.Errorf("%w: skew=%ds", ErrClockDrift, ts-nowUnix)
	}

	// Compute expected HMAC
	mac := hmac.New(sha256.New, secret)
	prefix := fmt.Sprintf("%d.", ts)
	mac.Write([]byte(prefix))
	mac.Write(rawBody)
	expectedHex := hex.EncodeToString(mac.Sum(nil))

	// Constant-time comparison
	if subtle.ConstantTimeCompare([]byte(expectedHex), []byte(strings.ToLower(v1Hex))) != 1 {
		return false, ErrSignatureMismatch
	}

	return true, nil
}
