# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem. Use GitHub's private reporting: **Security → Report a vulnerability** on this repository. If that is not available to you, write to alexis.diaz@dicapower.com with "noah security" in the subject.

Include the noah version (`noah version`), what you did, and what you expected. noah is maintained by one person: expect a first reply within about a week, and a fix or a clear answer as soon as it can be reproduced. There is no bug bounty.

## What counts

- Stored text that reaches the agent as an instruction even though it was flagged, or a way to get a flagged note injected automatically.
- A secret that is stored when `NOAH_SECRETS` is `redact` or `block`.
- Reading or writing memories of another project (space isolation).
- The installer or a release asset that does not match `SHA256SUMS`.
- The optional model reader (`internal/embed`) crashing or misbehaving on a crafted model file.

## What does not count

- The `⚠` detector missing a paraphrased attack. It is a heuristic, as the README says.
- Anything that needs write access to your own `~/.noah` folder or your own machine.

## Supported versions

Only the latest release gets fixes. Update with `install.sh`, `install.ps1`, Homebrew or Scoop; noah never updates itself.

## Verifying a download

Each release has a `SHA256SUMS` file. Check the archive you downloaded against it before running it (`shasum -a 256 -c SHA256SUMS --ignore-missing`). The optional meaning-based model is pinned to a fixed revision with checksums in the README.
