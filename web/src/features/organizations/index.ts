// Public surface of this feature. Cross-feature consumers MUST import from here.
// OrganizationManagement is a route, reached by the router's own lazy import: a
// barrel re-export would make it statically reachable from every module that
// imports this feature for the store, and its chunk would load on first paint.
export { useOrganizationStore, selectedOrganizationQuery } from './state/organization-store';
export { OrganizationPicker } from './OrganizationPicker';
