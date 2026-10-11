package httpserver

import (
	"bytes"
	"net/http"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"github.com/PMExtra/RedApp/internal/store"
)

// Private notes of vendors and applications. They have their own revision
// (1 before the first save) and never appear in other documents.

type adminNotesDTO struct {
	Text     string `json:"text"`
	HTML     string `json:"html"`
	Revision int64  `json:"revision"`
}

// notesMarkdown renders notes as GFM without raw HTML: goldmark's safe mode
// omits HTML blocks and inline HTML and drops dangerous link destinations.
// Table alignment uses the align attribute because the admin CSP rejects
// inline styles.
var notesMarkdown = goldmark.New(goldmark.WithExtensions(
	extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
	extension.Strikethrough, extension.Linkify, extension.TaskList,
))

func notesDTO(notes store.AdminNotes) adminNotesDTO {
	var html bytes.Buffer
	if err := notesMarkdown.Convert([]byte(notes.Text), &html); err != nil {
		html.Reset()
	}
	return adminNotesDTO{Text: notes.Text, HTML: html.String(), Revision: notes.Revision}
}

type adminNotesRequest struct {
	Text *string `json:"text"`
}

// notesTarget resolves the notes owner of the route: the store kind, key and
// the not-found code.
func notesTarget(r *http.Request) (kind, key string, notFound errorCode, e *apiError) {
	if r.PathValue("app") == "" {
		key, e = vendorParam(r)
		return "vendor", key, codeVendorNotFound, e
	}
	key, e = appParam(r)
	return "app", key, codeApplicationNotFound, e
}

// getNotes serves getVendorNotes and getAppNotes.
func (s *Server) getNotes(w http.ResponseWriter, r *http.Request) {
	kind, key, notFound, e := notesTarget(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	notes, err := s.store.AdminNotes(kind, key)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, notFound))
		return
	}
	writeRevision(w, http.StatusOK, notes.Revision, notesDTO(notes))
}

// replaceNotes serves replaceVendorNotes and replaceAppNotes.
func (s *Server) replaceNotes(w http.ResponseWriter, r *http.Request) {
	kind, key, notFound, e := notesTarget(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var in adminNotesRequest
	if e = decodeJSON(r, &in); e != nil {
		s.writeError(w, r, e)
		return
	}
	if in.Text == nil {
		s.fail(w, r, codeInvalidRequest, nil, "text is required")
		return
	}
	notes, err := s.store.SaveAdminNotes(kind, key, revision, *in.Text)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, notFound))
		return
	}
	writeRevision(w, http.StatusOK, notes.Revision, notesDTO(notes))
}
