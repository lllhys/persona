"""Docker Compose for checks that render deploy/docker/compose.yaml offline under a temporary HOME."""
import shutil
import subprocess

HINT = ("set PERSONA_TEST_DOCKER_CONFIG to a credential-free directory with cli-plugins/docker-compose "
        "(docs/operations/hub-update.md)")


def skip_without_compose(test, env):
    """Skip test unless `docker compose` runs in env. Under a temporary HOME, Docker looks for its CLI plugins in an
    empty config directory, and how it then fails depends on its version ("'compose' is not a docker command", or
    "unknown shorthand flag: 'f' in -f" from Docker 29 when the command line has -f): asking for the version tells
    every one of them apart from a Compose that runs."""
    if not shutil.which("docker", path=env.get("PATH")):
        test.skipTest("Docker Compose is unavailable; no containers are started by this check")
    try:
        result = subprocess.run(["docker", "compose", "version"], env=env, capture_output=True, text=True, timeout=15)
    except (OSError, subprocess.TimeoutExpired) as error:
        test.skipTest(f"Docker Compose is unavailable ({error}); {HINT}")
    if result.returncode != 0:
        said = (result.stderr.strip() or result.stdout.strip()).splitlines()
        test.skipTest(f"Docker Compose is unavailable ({said[0] if said else 'exit ' + str(result.returncode)}); {HINT}")
