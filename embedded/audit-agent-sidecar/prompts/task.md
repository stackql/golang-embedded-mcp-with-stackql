Run the golden-path audit for the GitHub organisation `{{ GITHUB_ORG }}`,
with `{{ GITHUB_REPO }}` as the repository under review. Read actual state
from the provider with the stackql tools; there is no state file. Use the
discovery tools before writing SQL rather than guessing table or column
names.

1. Inventory: the organisation's active (not archived) repositories
   (github.repos.repos with org), one list, kept for the next check: name,
   visibility, default branch, license (JSON_EXTRACT(license, '$.spdx_id')),
   stars. How many are there, and how many are public?
2. Licensing: every active public repository carries a license. Name the ones
   that do not; each is a finding.
3. Topics: `{{ GITHUB_REPO }}` carries the topics stackql, golang, mcp and
   golden-path (the topics column of the inventory row, or
   github.repos.topics with owner and repo). A missing topic is a finding.
4. Labels: `{{ GITHUB_REPO }}` has the standard labels needs-triage,
   security and agent-fixed (github.issues.labels with owner and repo). A
   missing label is a finding.
5. Branch protection: `{{ GITHUB_REPO }}` has an active ruleset named
   golden-path-default-branch targeting its default branch
   (github.repos.rules with owner and repo), or, failing that, the default
   branch reports protected = 1 in github.repos.branches. Neither is a
   finding.

Finish with a short plain-text report: one line per check, PASS or
ATTENTION, with one line of evidence each. For each ATTENTION, add the single
SQL statement that would fix it (INSERT for a label, REPLACE for topics,
INSERT for the ruleset), clearly marked as not run: this session is
read-only.
