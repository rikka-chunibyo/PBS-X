# Proposed job histories

Recommendation following device testing: give each local job an independent
snapshot history. Manual and scheduled runs of the same job append to that
same history. Folder and Shizuku choices belong to the job; scheduling is only
how a run is triggered. Editing a job revises the contents of its future
snapshots, rather than creating another identity merely because its schedule
or label changed.

A device namespace containing stable per-job namespaces fits PBS-X's existing
host identity model. For example, one device could use `devices/phone/photos`
and `devices/phone/documents`, with the same `host/<host-id>` backup ID in each.
Actual namespace components should include a stable job identifier, respect
PBS's length limits, and be stored when created so renaming a job does not
move its backups. Repeated chunks can still be deduplicated within the same
datastore across those groups.

This is a proposal, not an automatic storage migration in version 0.2.2.
Existing jobs currently retain their configured destinations. The Restore
catalogue lists all accessible groups, including old shared histories and
future per-job histories.

Automatic device keys cannot create arbitrary namespaces: PBS requires
datastore modification privileges to create them, and PBS-X additionally
authorizes generated keys against exact destinations in enabled server host
jobs. A future implementation should provision job namespaces through a
server-side operation restricted to an administrator-approved device parent,
and issue/update only the corresponding ACL grants. It must preserve the
normal existing-key/manual-namespace path and must not grant phones general
datastore administration.

Existing mixed histories should stay accessible in their original location.
Any move or split needs an explicit migration choice; no snapshots should be
silently moved, renamed or deleted. Snapshot notes remain useful labels within
a history, but labels alone do not isolate different job contents.
