package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// CreateTicket handles POST /api/ticket.
// Public endpoint used by a browser plugin to start a cross-domain login:
// the plugin creates a pending ticket, then opens the external browser with
// the ticket so the "other page" can bind its access_token to it later.
func CreateTicket(c *gin.Context) {
	ticket, err := service.CreateTicket()
	if err != nil {
		common.SysLog("create ticket failed: " + err.Error())
		common.ApiErrorMsg(c, "failed to create ticket")
		return
	}
	common.ApiSuccess(c, gin.H{
		"ticket": ticket,
	})
}

// GetTicket handles GET /api/ticket/:ticket.
// Public endpoint polled by the plugin. Returns "pending" while waiting for
// the external browser to bind, or "bound" together with the access_token and
// user info once the binding is done.
func GetTicket(c *gin.Context) {
	ticket := c.Param("ticket")
	data, err := service.GetTicket(ticket)
	if err != nil {
		if errors.Is(err, service.ErrTicketNotFound) {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		common.SysLog("get ticket failed: " + err.Error())
		common.ApiErrorMsg(c, "failed to get ticket")
		return
	}
	common.ApiSuccess(c, data)
}

// bindTicketRequest is the body of POST /api/user/ticket/bind.
type bindTicketRequest struct {
	Ticket string `json:"ticket"`
}

// BindTicket handles POST /api/user/ticket/bind (UserAuth required).
// The external-browser "other page" calls this with its own access_token
// (Authorization header + New-Api-User header) — or its session cookie — to
// bind its credentials to the pending ticket so the plugin can pick them up.
// A single user may bind multiple different tickets (one per plugin/domain),
// but each ticket can only be bound once.
func BindTicket(c *gin.Context) {
	var req bindTicketRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	if req.Ticket == "" {
		common.ApiErrorMsg(c, "ticket is required")
		return
	}

	userId := c.GetInt("id")
	if userId == 0 {
		common.ApiErrorMsg(c, "user not logged in")
		return
	}

	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.SysLog("bind ticket: get user failed: " + err.Error())
		common.ApiErrorMsg(c, "failed to bind ticket")
		return
	}
	if user.Status == common.UserStatusDisabled {
		common.ApiErrorMsg(c, "user is disabled")
		return
	}

	if err := service.BindTicketToUser(req.Ticket, user); err != nil {
		switch {
		case errors.Is(err, service.ErrTicketNotFound), errors.Is(err, service.ErrTicketAlreadyBound):
			common.ApiErrorMsg(c, err.Error())
		default:
			common.SysLog("bind ticket failed: " + err.Error())
			common.ApiErrorMsg(c, "failed to bind ticket")
		}
		return
	}

	common.ApiSuccess(c, gin.H{
		"status": service.TicketStatusBound,
	})
}
