package truapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/TruStory/octopus/services/truapi/db"
	"github.com/TruStory/octopus/services/truapi/truapi/cookies"
	"github.com/TruStory/octopus/services/truapi/truapi/render"
)

// AddCommentRequest represents the JSON request for adding a comment
type AddCommentRequest struct {
	ParentID   int64  `json:"parent_id,omitempty"`
	ClaimID    int64  `json:"claim_id,omitempty"`
	ArgumentID int64  `json:"argument_id,omitempty"`
	ElementID  int64  `json:"element_id,omitempty"`
	Body       string `json:"body"`
}

// HandleComment handles requests for comments
func (ta *TruAPI) HandleComment(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		ta.handleCreateComment(w, r)
	default:
		render.Error(w, r, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ta *TruAPI) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	request := &AddCommentRequest{}
	err := json.NewDecoder(r.Body).Decode(request)
	if err != nil {
		render.Error(w, r, "Error parsing request", http.StatusBadRequest)
		return
	}

	user, ok := r.Context().Value(userContextKey).(*cookies.AuthenticatedUser)
	if !ok || user == nil {
		render.Error(w, r, Err401NotAuthenticated.Error(), http.StatusUnauthorized)
		return
	}
	claim := ta.claimResolver(r.Context(), queryByClaimID{ID: uint64(request.ClaimID)})
	if claim.ID == 0 {
		render.Error(w, r, "Invalid claim", http.StatusBadRequest)
		return
	}
	comment := &db.Comment{
		ParentID:    request.ParentID,
		ClaimID:     request.ClaimID,
		CommunityID: claim.CommunityID,
		ArgumentID:  request.ArgumentID,
		ElementID:   request.ElementID,
		Body:        request.Body,
		Creator:     user.Address,
	}
	err = ta.DBClient.AddComment(comment)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ta.sendCommentNotification(CommentNotificationRequest{
		ID:         comment.ID,
		ClaimID:    comment.ClaimID,
		ArgumentID: comment.ArgumentID,
		ElementID:  comment.ElementID,
		Creator:    comment.Creator,
		Timestamp:  time.Now(),
	})

	ta.sendCommentToSlack(*comment)

	render.JSON(w, r, comment, http.StatusOK)
}
