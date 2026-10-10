package httpserver

import (
	"encoding/json"
	"net/http"

	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/store"
)

// Categories are created while saving application categories (new_categories)
// and removed automatically once no application or template uses them; this
// API lists, reads and renames them.

type categorySpecDTO struct {
	Name localizedText `json:"name"`
}

type categoryDTO struct {
	ID              string                    `json:"id"`
	Name            localizedText             `json:"name"`
	Builtin         bool                      `json:"builtin"`
	Applications    int64                     `json:"applications"`
	Revision        int64                     `json:"revision"`
	TemplateRef     *string                   `json:"template_ref"`
	TemplateHash    *string                   `json:"template_hash"`
	TemplateMissing bool                      `json:"template_missing"`
	Defaults        *categorySpecDTO          `json:"defaults"`
	Overrides       map[string]any            `json:"overrides"`
	Effective       categorySpecDTO           `json:"effective"`
	Fields          map[string]fieldOriginDTO `json:"fields"`
}

func categoryDocument(c store.CategoryListItem) categoryDTO {
	out := categoryDTO{ID: c.ID, Name: fromStoreText(c.Name), Builtin: c.Builtin, Applications: c.Applications, Revision: c.Revision,
		TemplateHash: c.TemplateHash, TemplateMissing: c.TemplateMissing, Overrides: map[string]any{}, Effective: categorySpecDTO{Name: fromStoreText(c.Name)},
		Fields: map[string]fieldOriginDTO{}}
	if c.Builtin {
		// Built-in categories are keyed by their ID in the embedded taxonomy.
		ref := c.ID
		out.TemplateRef = &ref
	}
	if c.Default != nil {
		out.Defaults = &categorySpecDTO{Name: fromStoreText(*c.Default)}
	}
	for key, value := range c.Overrides {
		out.Overrides[key] = value
	}
	for path, origin := range c.Fields {
		out.Fields[path] = fieldOriginDTO{Source: origin.Source, Differs: origin.Differs}
	}
	return out
}

func (s *Server) listCategories(w http.ResponseWriter, r *http.Request) {
	q, e := queryText(r, "q", 128)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	page, limit, e := pageQuery(r, 25)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	result, err := s.store.TaxonomyPage(q, page, limit)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	out := pageDTO[categoryDTO]{Items: make([]categoryDTO, 0, len(result.Items)), Page: result.Page, Limit: result.Limit, Total: result.Total, TotalPages: result.TotalPages}
	for _, c := range result.Items {
		out.Items = append(out.Items, categoryDocument(c))
	}
	writeOK(w, out)
}

func categoryParam(r *http.Request) (string, *apiError) {
	id := r.PathValue("category")
	if !identity.ValidSlug(id) {
		return "", newError(codeInvalidPath, nil, "Invalid category ID")
	}
	return id, nil
}

func (s *Server) getCategory(w http.ResponseWriter, r *http.Request) {
	id, e := categoryParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	c, err := s.store.Category(id)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeCategoryNotFound))
		return
	}
	writeRevision(w, http.StatusOK, c.Revision, categoryDocument(c))
}

type categoryPatchRequest struct {
	Set   map[string]json.RawMessage `json:"set"`
	Unset []string                   `json:"unset"`
}

func (s *Server) patchCategory(w http.ResponseWriter, r *http.Request) {
	id, e := categoryParam(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	revision, e := ifMatch(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	var in categoryPatchRequest
	if e = decodeJSON(r, &in); e != nil {
		s.writeError(w, r, e)
		return
	}
	for path := range in.Set {
		if path != "name.en" && path != "name.zh-CN" {
			s.fail(w, r, codeInvalidRequest, nil, "Unknown category path "+path)
			return
		}
	}
	if _, err := s.store.PatchTaxonomy(id, store.ConfigurationPatch{Revision: revision, Set: in.Set, Unset: in.Unset}); err != nil {
		s.writeError(w, r, directoryFailure(err, codeCategoryNotFound))
		return
	}
	c, err := s.store.Category(id)
	if err != nil {
		s.writeError(w, r, directoryFailure(err, codeCategoryNotFound))
		return
	}
	writeRevision(w, http.StatusOK, c.Revision, categoryDocument(c))
}
