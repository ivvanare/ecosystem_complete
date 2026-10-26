// Package audit writes the append-only security log (seg.tb_security_event).
//
// Two rules come straight from the model: the log never stores codes, tokens or passwords,
// and a failed attempt stores an HMAC of the email instead of the address — the clear value
// would be a user-enumeration oracle. The log also has no foreign key to tb_user, so it
// survives account deletion.
package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Event names. Kept as constants so a typo cannot silently create a new event kind.
const (
	EventSignupVerified     = "auth.signup.verified"
	EventSignupCodeRejected = "auth.signup.code_rejected"
	EventSessionCreated     = "auth.session.created"
	EventLoginSucceeded     = "auth.login.succeeded"
	EventLoginFailed        = "auth.login.failed"
	EventLoginLocked        = "auth.login.locked"
)

// Event is one row of the log.
type Event struct {
	Name      string
	UserID    string // subject: to whom it happened
	ActorID   string // who did it, when not the subject
	Email     string // hashed before it is written
	IP        string
	UserAgent string
	Metadata  map[string]any
}

// Recorder writes events. Implementations must be safe to call from a request path.
type Recorder interface {
	Record(ctx context.Context, event Event) error
}

// Postgres writes to seg.tb_security_event.
type Postgres struct {
	Pool *pgxpool.Pool
	// subkey is derived from the server secret so the audit HMAC never reuses the JWT key
	// directly.
	subkey []byte
}

// NewPostgres returns a recorder. The secret should be the same one the service uses for
// signing; it is only used to derive a separate HMAC key.
func NewPostgres(pool *pgxpool.Pool, secret []byte) *Postgres {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("melia/audit/email/v1"))
	return &Postgres{Pool: pool, subkey: mac.Sum(nil)}
}

// EmailHash is the HMAC stored in tx_email_hash. Exported for tests and for callers that
// need to search the log by address without storing it.
func (p *Postgres) EmailHash(email string) string {
	mac := hmac.New(sha256.New, p.subkey)
	mac.Write([]byte(normalize(email)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Record appends one event.
func (p *Postgres) Record(ctx context.Context, event Event) error {
	var emailHash any
	if event.Email != "" {
		emailHash = p.EmailHash(event.Email)
	}

	metadata := "{}"
	if len(event.Metadata) > 0 {
		encoded, err := json.Marshal(event.Metadata)
		if err != nil {
			return fmt.Errorf("audit: encoding metadata: %w", err)
		}
		metadata = string(encoded)
	}

	_, err := p.Pool.Exec(ctx, `
		INSERT INTO seg.tb_security_event
			(co_security_event, tx_event, co_user, co_actor_user, tx_email_hash,
			 tx_ip, tx_user_agent, js_metadata, dt_created_at)
		VALUES (gen_random_uuid(), $1,
			NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4,
			NULLIF($5, ''), NULLIF($6, ''), $7::jsonb, now())`,
		event.Name, event.UserID, event.ActorID, emailHash, event.IP, event.UserAgent, metadata,
	)
	if err != nil {
		return fmt.Errorf("audit: inserting event %s: %w", event.Name, err)
	}
	return nil
}

func normalize(email string) string {
	lower := make([]byte, 0, len(email))
	for i := 0; i < len(email); i++ {
		c := email[i]
		if c == ' ' || c == '\t' || c == '\n' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		lower = append(lower, c)
	}
	return string(lower)
}
