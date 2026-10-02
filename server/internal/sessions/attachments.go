// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package sessions

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
)

// ErrNotImage is returned for uploads that are not an accepted image type.
var ErrNotImage = errors.New("only PNG, JPEG, GIF and WebP images are accepted")

// SaveAttachment stores an image for a session. The media type is sniffed from the bytes, never taken
// from the client.
func (h *Hub) SaveAttachment(ctx context.Context, org, sessionID, userID string, data []byte) (*models.Attachment, error) {
	s, err := h.Get(ctx, org, sessionID)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("the image is empty")
	}
	if len(data) > proto.MaxImageBytes {
		return nil, fmt.Errorf("the image is larger than %d MB", proto.MaxImageBytes>>20)
	}
	mt := http.DetectContentType(data)
	if !proto.IsImageType(mt) {
		return nil, ErrNotImage
	}
	a := &models.Attachment{Base: models.Base{ID: models.NewID("att"), OrganizationID: org}, SessionID: s.ID,
		MediaType: mt, Size: len(data), CreatedBy: userID, Data: data}
	if err := h.db.WithContext(ctx).Create(a).Error; err != nil {
		return nil, err
	}
	h.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "session.attachment",
		TargetType: "session", TargetID: s.ID, Metadata: map[string]any{"attachment": a.ID, "media_type": mt, "bytes": len(data)}})
	return a, nil
}

// Attachment loads one of a session's attachments, bytes included.
func (h *Hub) Attachment(ctx context.Context, org, sessionID, id string) (*models.Attachment, error) {
	var a models.Attachment
	if err := h.db.WithContext(ctx).First(&a, "id = ? AND session_id = ? AND organization_id = ?", id, sessionID, org).Error; err != nil {
		return nil, ErrNotFound
	}
	return &a, nil
}

// imageRefs turns attachment ids posted with a message into image references, refusing ids that are
// not this session's.
func (h *Hub) imageRefs(ctx context.Context, s *models.ChatSession, ids []string) ([]proto.ImageSource, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > proto.MaxImagesPerMessage {
		return nil, fmt.Errorf("at most %d images per message", proto.MaxImagesPerMessage)
	}
	var rows []models.Attachment
	if err := h.db.WithContext(ctx).Select("id", "media_type").
		Where("id IN ? AND session_id = ? AND organization_id = ?", ids, s.ID, s.OrganizationID).Find(&rows).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]string, len(rows))
	for _, r := range rows {
		byID[r.ID] = r.MediaType
	}
	refs := make([]proto.ImageSource, 0, len(ids))
	for _, id := range ids {
		mt, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown attachment %q", id)
		}
		refs = append(refs, proto.ImageSource{AttachmentID: id, MediaType: mt})
	}
	return refs, nil
}

// ResolveImages fills in the bytes of a session's image blocks for a provider call. History comes from
// the agent, so only this session's attachments are loaded and any bytes the agent sent are replaced;
// an image that cannot be resolved becomes a text note instead.
func (h *Hub) ResolveImages(ctx context.Context, s *models.ChatSession, msgs []proto.Message) []proto.Message {
	var ids []string
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == proto.BlockImage && b.Source != nil {
				ids = append(ids, b.Source.AttachmentID)
			}
		}
	}
	if len(ids) == 0 {
		return msgs
	}
	var rows []models.Attachment
	h.db.WithContext(ctx).Where("id IN ? AND session_id = ? AND organization_id = ?", ids, s.ID, s.OrganizationID).Find(&rows)
	data := make(map[string]models.Attachment, len(rows))
	for _, r := range rows {
		data[r.ID] = r
	}
	out := make([]proto.Message, len(msgs))
	for i, m := range msgs {
		out[i] = proto.Message{Role: m.Role, Content: make([]proto.Block, len(m.Content))}
		for j, b := range m.Content {
			if b.Type == proto.BlockImage {
				b = imageBlock(b, data)
			}
			out[i].Content[j] = b
		}
	}
	return out
}

func imageBlock(b proto.Block, data map[string]models.Attachment) proto.Block {
	if b.Source != nil {
		if a, ok := data[b.Source.AttachmentID]; ok {
			return proto.Block{Type: proto.BlockImage, Source: &proto.ImageSource{AttachmentID: a.ID, MediaType: a.MediaType,
				Data: base64.StdEncoding.EncodeToString(a.Data)}}
		}
	}
	return proto.Block{Type: proto.BlockText, Text: "[image unavailable]"}
}
