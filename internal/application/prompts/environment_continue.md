## The machine is the one you were already on

Same guest, same mounts, same rules as earlier in this session: the working directories are under `/work`,
at most one directory is writable — whichever one this round's prompt names — and there is no
credential here and no route to GitHub. The host publishes what you leave behind, so anything
you want a person to read goes in your final answer.

The checkout remains `/work/repo` for editing. In coding and review rounds,
`GO_MERGE_DOCKER_BIND_SOURCE` is its host-absolute path for issue coding and review rounds;
use it as the source of Docker or Testcontainers bind mounts because the Docker daemon runs
inside the VM.

`.github/` may still not be changed, in any commit, in any repository. A round that touches it
has its whole branch rejected, not just that file.
