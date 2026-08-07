package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/device"
)

const msgSiteNotFound = "site not found"

// CreateSite implements StrictServerInterface. Adding a site reshapes how the
// fleet is filed, so it sits behind the admin gate.
func (s *Server) CreateSite(ctx context.Context, request CreateSiteRequestObject) (CreateSiteResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, CreateSite403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	if request.Body.Name == "" {
		return CreateSite400JSONResponse{Error: "name is required"}, nil
	}
	if msg := invalidText("name", request.Body.Name, maxSiteNameLen); msg != "" {
		return CreateSite400JSONResponse{Error: msg}, nil
	}

	// A create that names no customer takes the tenant's own, so a technician who
	// has not picked one still gets a site somewhere valid rather than an error.
	site := &device.Site{ID: uuid.New(), Name: request.Body.Name}
	if request.Body.OrganizationId != nil {
		site.OrganizationID = *request.Body.OrganizationId
	}

	switch err := s.sites.Create(ctx, site); {
	case err == nil:
	case errors.Is(err, device.ErrOrganizationNotFound):
		return CreateSite400JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, device.ErrSiteNameTaken):
		return CreateSite400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}

	return CreateSite201JSONResponse(siteToAPI(site)), nil
}

// ListSites implements StrictServerInterface. Sites are a fleet read: every
// member of the tenant sees every site in it, narrowed to one customer when the
// caller names one.
func (s *Server) ListSites(ctx context.Context, request ListSitesRequestObject) (ListSitesResponseObject, error) {
	var organizationID device.OrganizationID
	if request.Params.OrganizationId != nil {
		organizationID = *request.Params.OrganizationId
	}

	sites, err := s.sites.List(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	return ListSites200JSONResponse(sitesToAPI(sites)), nil
}

// GetSite implements StrictServerInterface.
func (s *Server) GetSite(ctx context.Context, request GetSiteRequestObject) (GetSiteResponseObject, error) {
	site, err := s.sites.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, device.ErrSiteNotFound) {
			return GetSite404JSONResponse{Error: msgSiteNotFound}, nil
		}
		return nil, err
	}

	return GetSite200JSONResponse(siteToAPI(site)), nil
}

// DeleteSite implements StrictServerInterface. Removing a site reshapes how
// the fleet is filed, so it sits behind the admin gate.
func (s *Server) DeleteSite(ctx context.Context, request DeleteSiteRequestObject) (DeleteSiteResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, DeleteSite403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	if _, err := s.sites.Get(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrSiteNotFound) {
			return DeleteSite404JSONResponse{Error: msgSiteNotFound}, nil
		}
		return nil, err
	}

	if err := s.sites.Delete(ctx, request.Id); err != nil {
		return nil, err
	}

	return DeleteSite204Response{}, nil
}
