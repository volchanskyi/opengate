package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/organization"
)

const (
	msgOrganizationNotFound = "organization not found"
	msgLastOrganization     = "a tenant must keep at least one organization"
)

// ListOrganizations implements StrictServerInterface. The customer list is a
// fleet read: every member of a tenant sees every customer in it, because the
// organization is a targeting level and the tenant is the boundary.
//
// It also settles the no-orphan floor at the point the list is first asked for:
// a tenant with no customers gets its own, so the picker is never empty and a
// device always has somewhere to belong.
func (s *Server) ListOrganizations(ctx context.Context, request ListOrganizationsRequestObject) (ListOrganizationsResponseObject, error) {
	if _, err := s.organizations.EnsureDefault(ctx); err != nil {
		return nil, err
	}

	includeArchived := request.Params.IncludeArchived != nil && *request.Params.IncludeArchived
	orgs, err := s.organizations.List(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	return ListOrganizations200JSONResponse(organizationsToAPI(orgs)), nil
}

// GetOrganization implements StrictServerInterface.
func (s *Server) GetOrganization(ctx context.Context, request GetOrganizationRequestObject) (GetOrganizationResponseObject, error) {
	org, err := s.organizations.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, organization.ErrNotFound) {
			return GetOrganization404JSONResponse{Error: msgOrganizationNotFound}, nil
		}
		return nil, err
	}
	return GetOrganization200JSONResponse(organizationToAPI(org)), nil
}

// CreateOrganization implements StrictServerInterface. Taking on a customer
// reshapes who the fleet is for, so it sits behind the admin gate.
func (s *Server) CreateOrganization(ctx context.Context, request CreateOrganizationRequestObject) (CreateOrganizationResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, CreateOrganization403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if msg := invalidText("name", request.Body.Name, organization.MaxNameLen); msg != "" {
		return CreateOrganization400JSONResponse{Error: msg}, nil
	}

	org := &organization.Organization{ID: uuid.New(), Name: request.Body.Name}
	switch err := s.organizations.Create(ctx, org); {
	case err == nil:
	case errors.Is(err, organization.ErrNameTaken):
		return CreateOrganization409JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, organization.ErrNameRequired):
		return CreateOrganization400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}
	return CreateOrganization201JSONResponse(organizationToAPI(org)), nil
}

// UpdateOrganization implements StrictServerInterface. A rename, an
// archive/restore, or both; an omitted field is left alone.
func (s *Server) UpdateOrganization(ctx context.Context, request UpdateOrganizationRequestObject) (UpdateOrganizationResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, UpdateOrganization403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	resp, err := s.applyOrganizationUpdate(ctx, request)
	if resp != nil || err != nil {
		return resp, err
	}

	org, err := s.organizations.Get(ctx, request.Id)
	if err != nil {
		return organizationUpdateFailure(err)
	}
	return UpdateOrganization200JSONResponse(organizationToAPI(org)), nil
}

// applyOrganizationUpdate applies whichever halves the request carries. It
// answers with the response to send when a half could not be applied, or with
// two nils to say the caller should read the customer back and return it.
func (s *Server) applyOrganizationUpdate(ctx context.Context, request UpdateOrganizationRequestObject) (UpdateOrganizationResponseObject, error) {
	if request.Body.Name != nil {
		if msg := invalidText("name", *request.Body.Name, organization.MaxNameLen); msg != "" {
			return UpdateOrganization400JSONResponse{Error: msg}, nil
		}
		if err := s.organizations.Rename(ctx, request.Id, *request.Body.Name); err != nil {
			return organizationUpdateFailure(err)
		}
	}
	if request.Body.Archived != nil {
		if err := s.organizations.SetArchived(ctx, request.Id, *request.Body.Archived); err != nil {
			return organizationUpdateFailure(err)
		}
	}
	return nil, nil
}

// organizationUpdateFailure maps a repository error to the response the update
// should answer with. Anything it does not recognise is returned as an error so
// the middleware answers 500 rather than dressing an internal fault up as a
// client mistake and echoing its text back.
func organizationUpdateFailure(err error) (UpdateOrganizationResponseObject, error) {
	switch {
	case errors.Is(err, organization.ErrNotFound):
		return UpdateOrganization404JSONResponse{Error: msgOrganizationNotFound}, nil
	case errors.Is(err, organization.ErrNameTaken):
		return UpdateOrganization409JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, organization.ErrNameRequired):
		return UpdateOrganization400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}
}

// DeleteOrganization implements StrictServerInterface. Deleting a customer takes
// its devices with it, so it is admin-gated — and a tenant's last customer is
// refused, because a device must always have somewhere to belong.
func (s *Server) DeleteOrganization(ctx context.Context, request DeleteOrganizationRequestObject) (DeleteOrganizationResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, DeleteOrganization403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	all, err := s.organizations.List(ctx, true)
	if err != nil {
		return nil, err
	}
	if len(all) <= 1 {
		return DeleteOrganization409JSONResponse{Error: msgLastOrganization}, nil
	}

	if err := s.organizations.Delete(ctx, request.Id); err != nil {
		if errors.Is(err, organization.ErrNotFound) {
			return DeleteOrganization404JSONResponse{Error: msgOrganizationNotFound}, nil
		}
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "organization.delete", request.Id.String(), "customer and its devices erased")
	return DeleteOrganization204Response{}, nil
}
