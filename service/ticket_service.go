package service

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/go-redis/redis/v8"
)

// Ticket exchange status constants.
const (
	TicketStatusPending = "pending"
	TicketStatusBound   = "bound"

	ticketKeyPrefix  = "ticket:exchange:"
	ticketLength     = 32
	ticketPendingTTL = 10 * time.Minute
	ticketBoundTTL   = 5 * time.Minute
)

// Errors for the ticket exchange flow.
var (
	ErrTicketNotFound     = errors.New("ticket not found or expired")
	ErrTicketAlreadyBound = errors.New("ticket already bound")
)

// TicketData is the persisted (Redis / in-memory) state of a login ticket.
// When Status == "pending", only Status/CreatedAt are populated.
// When Status == "bound", all user-related fields are populated so the
// polling plugin can pick them up.
type TicketData struct {
	Status      string `json:"status"`
	AccessToken string `json:"access_token,omitempty"`
	UserID      int    `json:"user_id,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Role        int    `json:"role,omitempty"`
	UserStatus  int    `json:"user_status,omitempty"`
	Group       string `json:"group,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

// ticketEntry is the in-memory fallback record used when Redis is disabled.
type ticketEntry struct {
	data      *TicketData
	expiresAt time.Time
}

var (
	ticketStore      = make(map[string]*ticketEntry)
	ticketStoreMutex sync.Mutex
)

// CreateTicket generates a new pending ticket and persists it.
func CreateTicket() (string, error) {
	ticket, err := common.GenerateRandomCharsKey(ticketLength)
	if err != nil {
		return "", fmt.Errorf("generate ticket: %w", err)
	}
	data := &TicketData{
		Status:    TicketStatusPending,
		CreatedAt: common.GetTimestamp(),
	}
	if err := saveTicket(ticket, data, ticketPendingTTL); err != nil {
		return "", err
	}
	return ticket, nil
}

// GetTicket returns the current state of a ticket.
// Returns ErrTicketNotFound when the ticket is missing or expired.
func GetTicket(ticket string) (*TicketData, error) {
	if ticket == "" {
		return nil, ErrTicketNotFound
	}
	if common.RedisEnabled {
		raw, err := common.RedisGet(ticketKeyPrefix + ticket)
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return nil, ErrTicketNotFound
			}
			return nil, err
		}
		var data TicketData
		if err := common.UnmarshalJsonStr(raw, &data); err != nil {
			return nil, fmt.Errorf("unmarshal ticket: %w", err)
		}
		return &data, nil
	}
	ticketStoreMutex.Lock()
	defer ticketStoreMutex.Unlock()
	entry, ok := ticketStore[ticket]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(ticketStore, ticket)
		}
		return nil, ErrTicketNotFound
	}
	return entry.data, nil
}

// BindTicketToUser binds the given user's access_token to a pending ticket.
// On success the ticket transitions pending -> bound and is kept for
// ticketBoundTTL so the polling plugin has a window to pick the token up.
//
// The access_token is generated on demand when the user does not have one,
// mirroring setupLogin's algorithm without modifying the original login flow.
//
// Note: the check-then-update is not atomic under Redis, but for this flow it
// is acceptable — each ticket is single-use and the bound payload is identical
// for the same user, so a rare concurrent retry only overwrites with the same
// value.
func BindTicketToUser(ticket string, user *model.User) error {
	if ticket == "" {
		return ErrTicketNotFound
	}
	if user == nil || user.Id == 0 {
		return errors.New("invalid user")
	}

	accessToken := user.GetAccessToken()
	if accessToken == "" {
		randI := common.GetRandomInt(4)
		key, err := common.GenerateRandomKey(29 + randI)
		if err != nil {
			return fmt.Errorf("generate access token: %w", err)
		}
		if err := model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("access_token", key).Error; err != nil {
			return fmt.Errorf("save access token: %w", err)
		}
		user.SetAccessToken(key)
		accessToken = key
	}

	bound := &TicketData{
		Status:      TicketStatusBound,
		AccessToken: accessToken,
		UserID:      user.Id,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		UserStatus:  user.Status,
		Group:       user.Group,
		CreatedAt:   common.GetTimestamp(),
	}

	if common.RedisEnabled {
		raw, err := common.RedisGet(ticketKeyPrefix + ticket)
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return ErrTicketNotFound
			}
			return err
		}
		var existing TicketData
		if err := common.UnmarshalJsonStr(raw, &existing); err != nil {
			return fmt.Errorf("unmarshal ticket: %w", err)
		}
		if existing.Status != TicketStatusPending {
			return ErrTicketAlreadyBound
		}
		return saveTicket(ticket, bound, ticketBoundTTL)
	}

	ticketStoreMutex.Lock()
	defer ticketStoreMutex.Unlock()
	cleanupExpiredTicketsLocked()
	entry, ok := ticketStore[ticket]
	if !ok {
		return ErrTicketNotFound
	}
	if entry.data.Status != TicketStatusPending {
		return ErrTicketAlreadyBound
	}
	ticketStore[ticket] = &ticketEntry{data: bound, expiresAt: time.Now().Add(ticketBoundTTL)}
	return nil
}

// saveTicket persists the ticket data with the given TTL.
func saveTicket(ticket string, data *TicketData, ttl time.Duration) error {
	if common.RedisEnabled {
		raw, err := common.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal ticket: %w", err)
		}
		return common.RedisSet(ticketKeyPrefix+ticket, string(raw), ttl)
	}
	ticketStoreMutex.Lock()
	defer ticketStoreMutex.Unlock()
	cleanupExpiredTicketsLocked()
	ticketStore[ticket] = &ticketEntry{data: data, expiresAt: time.Now().Add(ttl)}
	return nil
}

// cleanupExpiredTicketsLocked purges expired entries.
// Caller must hold ticketStoreMutex.
func cleanupExpiredTicketsLocked() {
	now := time.Now()
	for k, v := range ticketStore {
		if now.After(v.expiresAt) {
			delete(ticketStore, k)
		}
	}
}
