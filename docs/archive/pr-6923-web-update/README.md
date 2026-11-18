# Web development update evidence for PR #6923

The 29 original browser captures and their checksums remain in the published history
at commit `87d0ca342764ffbe2d7b95703e397c735213d58f`, with the later record at
`dbd9809511e8a59e2e42f06a46ad83cd77166c64`. They describe Web source tree
`bc453ef2d738f54b53af919951c4f3b95eceb656`. They are historical evidence, not fresh
captures of the later upstream synchronization.

[PR #6923](https://github.com/nwverse/persona/pull/6923) retains the fixed-commit gallery
and the validation reports. Review images are removed from the final repository tree
under the current collaboration rules; their published commits remain reachable.
The original images cover the full App and Hub settings in light/dark, wide/narrow,
standard/Large text and update/backup/confirmation states, using a synthetic local API.
They do not establish production update or restoration behavior.

The [recovery review record](../operations/2026-11-16-web-update-review-6923.md)
describes backend reproductions and remaining acceptance boundaries. Later verification
is bound to its own exact source revision in the PR. Author inspection does not replace
maintainer review or human visual approval.
