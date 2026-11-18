"""Bounded recovery of watcher-owned worktrees and Git fetch debris."""
import json
import os
import re
import shutil
import time
from pathlib import Path

from pr_watch_common import WatchError, bounded, checked, run

STORAGE_ERROR = re.compile(r"(?:no space left|out of diskspace|\bENOSPC\b)", re.I)
OBJECT_ERROR = re.compile(r"(?:object file|corrupt|missing necessary objects|invalid index-pack|unable to read [0-9a-f]{40}|bad object)", re.I)


def owned_root(state_file):
    return Path(state_file).resolve().parent / "worktrees"


def owned_path(state_file, path):
    """Only a real direct child of the watcher's private directory is removable."""
    root = owned_root(state_file)
    candidate = Path(path)
    resolved = candidate.resolve(strict=False)
    if candidate.is_symlink() or candidate.parent != root or resolved.parent != root or resolved == root:
        raise WatchError("refusing to clean a path outside watcher-owned worktrees")
    return resolved


def atomic_leases(state_file, data):
    """Store private lease state with the same atomic replacement as PR evidence."""
    path = Path(state_file)
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(json.dumps(data, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    os.chmod(temporary, 0o600)
    temporary.replace(path)


def reclaim_worktrees(checkout, state_file, protected=(), older_than=86400, state=None, repo=None):
    """Reclaim expired leases using one lock-protected state snapshot."""
    if state is not None:
        state.lock.acquire()
    try:
        current = state.data if state is not None else json.loads(Path(state_file).read_text(encoding="utf-8"))
        leases = current.get("worktree_leases", {})
        keep = {Path(path).resolve(strict=False) for path in protected}
        candidates = []
        for name, lease in leases.items():
            if name.endswith("-base-check"):
                continue
            try:
                path = owned_path(state_file, name)
                companion = Path(str(path) + "-base-check")
                companion_lease = leases.get(str(companion))
                age_leases = (lease,) if companion_lease is None else (lease, companion_lease)
                expired = all(time.time() - float(item.get("heartbeat", 0)) >= older_than
                              for item in age_leases)
            except (WatchError, TypeError, ValueError, OSError):
                continue
            if (path in keep or companion in keep or lease.get("active")
                    or companion_lease is not None and companion_lease.get("active")
                    or repo is not None and (lease.get("repository") != repo or companion_lease is not None and companion_lease.get("repository") not in (None, repo))
                    or not expired):
                continue
            candidates.append(path)
        if not candidates:
            return 0
        removed = 0
        for path in candidates:
            companion = Path(str(path) + "-base-check")
            lease = leases.get(str(path), {})
            companion_lease = leases.get(str(companion))
            try:
                age_leases = (lease,) if companion_lease is None else (lease, companion_lease)
                expired = all(time.time() - float(item.get("heartbeat", 0)) >= older_than
                              for item in age_leases)
            except (TypeError, ValueError, OSError):
                continue
            if (path in keep or companion in keep or lease.get("active")
                    or companion_lease is not None and companion_lease.get("active")
                    or repo is not None and (lease.get("repository") != repo or companion_lease is not None and companion_lease.get("repository") not in (None, repo))
                    or not expired):
                continue
            try:
                existed = path.exists() or companion.exists()
                if companion.exists():
                    remove_worktree(checkout, state_file, companion)
                if path.exists():
                    remove_worktree(checkout, state_file, path)
                if not path.exists() and not companion.exists():
                    leases.pop(str(path), None)
                    leases.pop(str(companion), None)
                    if existed:
                        removed += 1
                    if state is not None:
                        try:
                            atomic_leases(state_file, current)
                        except OSError:
                            # The confirmed removal still freed storage. Keep
                            # the in-memory lease cleanup for the next write;
                            # a metadata outage must not undo that recovery.
                            pass
            except (WatchError, OSError):
                continue
        run(["git", "worktree", "prune", "--expire", "now"], cwd=checkout, timeout=90)
        return removed
    finally:
        if state is not None:
            state.lock.release()


def registration_path(checkout, candidate):
    """Resolve only this checkout's own administrative worktree file."""
    common_text = checked(["git", "rev-parse", "--git-common-dir"], cwd=checkout).strip()
    common = (Path(checkout).resolve() / common_text).resolve() if not Path(common_text).is_absolute() else Path(common_text).resolve()
    destination = common / "worktrees" / candidate.name
    if not destination.exists():
        return None
    marker = candidate / ".git"
    if marker.is_file() and not marker.is_symlink():
        text = marker.read_text(encoding="utf-8").strip()
        if text.startswith("gitdir: "):
            referenced = Path(text[len("gitdir: "):])
            if not referenced.is_absolute():
                referenced = candidate / referenced
            if referenced.resolve(strict=False) != destination.resolve(strict=False):
                raise WatchError("worktree registration does not match the owned checkout")
    return destination


def remove_worktree(checkout, state_file, path):
    """Remove one known checkout; never delete a still-registered Git worktree."""
    if not path:
        return
    candidate = owned_path(state_file, path)
    registration = registration_path(checkout, candidate)
    result = run(["git", "worktree", "remove", "--force", str(candidate)], cwd=checkout, timeout=90)
    if result.returncode:
        if candidate.exists():
            if registration is not None and registration.exists():
                raise WatchError("refusing to delete a registered worktree: " + bounded(result.stderr, 800))
            # A missing registration is the only case where a directory can be
            # residual debris. Prune stale metadata, then verify once more.
            run(["git", "worktree", "prune", "--expire", "now"], cwd=checkout, timeout=90)
            if registration is not None and registration.exists():
                raise WatchError("worktree registration remains after pruning: " + bounded(result.stderr, 800))
            if candidate.exists():
                # The caller created this exact mkdtemp reservation. Once Git
                # no longer registers it, clean only that watcher-owned path;
                # no other directory is eligible for recursive deletion.
                shutil.rmtree(candidate)
    run(["git", "worktree", "prune", "--expire", "now"], cwd=checkout, timeout=90)
    if candidate.exists():
        raise WatchError("Git returned success but the watcher-owned worktree still exists")


def recover_object_debris(checkout):
    """Discard only incomplete local objects; refetching restores their contents.

    Never prune reachable objects, expire reflogs, reset a checkout, or remove
    the shared object database. A corrupt nonempty object remains a fail-closed
    error for the caller rather than being mistaken for repaired source code.
    """
    checkout = Path(checkout).resolve()
    common_text = checked(["git", "rev-parse", "--git-common-dir"], cwd=checkout).strip()
    common = (checkout / common_text).resolve() if not Path(common_text).is_absolute() else Path(common_text).resolve()
    if common.name != ".git" or not common.is_dir() or common.is_symlink():
        raise WatchError("refusing to repair objects outside the trusted Git common directory")
    objects = common / "objects"
    removed = 0
    for directory in objects.iterdir() if objects.is_dir() else ():
        if not re.fullmatch(r"[0-9a-f]{2}", directory.name) or not directory.is_dir() or directory.is_symlink():
            continue
        for item in directory.iterdir():
            if (re.fullmatch(r"[0-9a-f]{38}", item.name) and item.is_file()
                    and not item.is_symlink() and item.stat().st_size == 0):
                item.unlink()
                removed += 1
    pack = objects / "pack"
    # Temporary pack files can belong to long-running index-pack operations;
    # age alone is not proof of abandonment and never authorizes deletion.
    return removed


def recover_worktree_failure(checkout, state_file, error, protected=(), state=None, repo=None):
    """Repair a known infrastructure condition and report whether to retry once."""
    if STORAGE_ERROR.search(str(error)):
        removed = reclaim_worktrees(checkout, state_file, protected, state=state, repo=repo)
        if not removed:
            return False
        return True
    if OBJECT_ERROR.search(str(error)):
        return recover_object_debris(checkout) > 0
    return False
