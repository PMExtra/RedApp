package httpserver

import (
	"net/http"
	"slices"

	"github.com/PMExtra/RedApp/internal/identity"
)

// directoryQuery reads the q and state filters shared by listVendors and listApps.
func directoryQuery(r *http.Request) (q, state string, e *apiError) {
	if q, e = queryText(r, "q", 128); e != nil {
		return
	}
	state = r.URL.Query().Get("state")
	if state == "" {
		state = "current"
	}
	if !slices.Contains([]string{"current", "enabled", "disabled", "deleted"}, state) {
		return "", "", newError(codeInvalidQuery, nil, "state must be current, enabled, disabled or deleted")
	}
	return q, state, nil
}

// listVendors returns one page of vendors, each with a preview of at most five
// matching applications.
func (s *Server) listVendors(w http.ResponseWriter, r *http.Request) {
	q, state, e := directoryQuery(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	page, limit, e := pageQuery(r, 12)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	result, err := s.store.DirectoryPage(page, limit, q, state)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	out := pageDTO[vendorListItemDTO]{Items: make([]vendorListItemDTO, 0, len(result.Items)), Page: result.Page, Limit: result.Limit, Total: result.Total, TotalPages: result.TotalPages}
	for _, card := range result.Items {
		item := vendorListItemDTO{vendorDTO: vendorDocument(card.Vendor), Apps: make([]appDTO, 0, len(card.Apps)), AppTotal: card.AppTotal}
		for _, a := range card.Apps {
			item.Apps = append(item.Apps, appDocument(a))
		}
		out.Items = append(out.Items, item)
	}
	writeOK(w, out)
}

// listApps returns one page of applications, optionally of one vendor, sorted
// over all matching applications before paging (see directory_table.go).
func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	q, state, e := directoryQuery(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	page, limit, e := pageQuery(r, 20)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	order, e := tableOrderQuery(r)
	if e != nil {
		s.writeError(w, r, e)
		return
	}
	vendor := r.URL.Query().Get("vendor")
	if vendor != "" {
		if !identity.ValidVendor(vendor) {
			s.fail(w, r, codeInvalidQuery, nil, "vendor must be a vendor ID")
			return
		}
		if _, err := s.store.Vendor(vendor); err != nil {
			s.writeError(w, r, directoryFailure(err, codeVendorNotFound))
			return
		}
	}
	rows, err := s.applicationTable(vendor, q, state, order)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	out := pageDTO[appListItemDTO]{Items: []appListItemDTO{}, Page: page, Limit: limit, Total: int64(len(rows))}
	out.TotalPages = max(1, (len(rows)+limit-1)/limit)
	start := min((page-1)*limit, len(rows))
	end := min(start+limit, len(rows))
	out.Items = append(out.Items, rows[start:end]...)
	writeOK(w, out)
}
