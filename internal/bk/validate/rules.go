package validate

// RULES blacklist rule for service account role
const RULES = `rules:
- apiGroups:
  - '*'
  resources:
  - nodes
  - networkpolicies
  verbs:
  - create
  - update
  - patch
  - delete
  - deletecollection
`
