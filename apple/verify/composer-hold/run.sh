#!/bin/bash
# Model-check the TLA+ spec in this directory with TLC and compare every run with expect.txt.
# Copy into verify/<target>/ unchanged; describe the target only in the files next to it:
#   <Spec>.tla     the one spec in this directory
#   <run>.cfg      one TLC config per run
#   expect.txt     "<run> PASS|FAIL" per line; an optional "# cols: <var> ..." line picks the state
#                  columns trace.py prints for each step of a counterexample
#   COVERAGE.txt   "<sync point> <path> <what the spec covers>" per covered file; the sync point is a
#                  git commit, or the file's sha256 when it is not in git; paths are relative to here
# Usage: run.sh [run ...]   every run in expect.txt, or only the named ones
#        run.sh --fetch     download TLA+ tools $VERSION to $JAR and check its sha256
# Each run writes the command and TLC's output to runs/<run>.out and the counterexample, one line per
# step, to runs/<run>.trace.txt (keep runs/ out of git).
# Exit 0: every run as expected. 1: a run is not, or TLC is missing. 2: every run as expected, but a
# covered file changed after its sync point: re-read that diff before trusting a PASS.
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
VERSION=1.7.4
SHA256=936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88
JAR=${TLA2TOOLS:-$HOME/.cache/tla2tools/$VERSION/tla2tools.jar}
sha() { { command -v shasum >/dev/null && shasum -a 256 "$1" || sha256sum "$1"; } 2>/dev/null | cut -d' ' -f1; }

if [ "${1:-}" = --fetch ]; then
  mkdir -p "$(dirname "$JAR")"
  curl -fsSL -o "$JAR.part" "https://github.com/tlaplus/tlaplus/releases/download/v$VERSION/tla2tools.jar" \
    || { echo "FAIL download of tla2tools.jar $VERSION"; exit 1; }
  got=$(sha "$JAR.part")
  [ "$got" = "$SHA256" ] || { echo "FAIL sha256 is $got, expected $SHA256 (download kept at $JAR.part)"; exit 1; }
  mv "$JAR.part" "$JAR" && echo "OK $JAR"
  exit 0
fi
[ -f "$JAR" ] || { echo "FAIL no $JAR: run $0 --fetch, or set TLA2TOOLS"; exit 1; }
[ "$(sha "$JAR")" = "$SHA256" ] || { echo "FAIL $JAR is not TLA+ tools $VERSION (sha256 differs)"; exit 1; }

drift=0
while read -r sync file what; do
  case "$sync" in ''|'#'*) continue ;; esac
  case "$file" in /*) ;; *) file=$HERE/$file ;; esac
  if [ ${#sync} -eq 64 ]; then
    [ "$(sha "$file")" = "$sync" ] && continue
  elif git -C "$(dirname "$file")" diff --quiet "$sync" -- "$file" 2>/dev/null; then
    continue   # the file (committed or not) is as it was at the sync commit
  fi
  echo "WARN DRIFT $file changed since $sync: re-read the diff for $what"
  drift=1
done < "${FV_COVERAGE:-$HERE/COVERAGE.txt}"   # FV_COVERAGE: only to test this check

spec=$(cd "$HERE" && ls ./*.tla 2>/dev/null)
[ "$(printf '%s\n' "$spec" | grep -c .)" = 1 ] || { echo "FAIL want exactly one .tla in $HERE"; exit 1; }
cols=$(sed -n 's/^# cols: *//p' "$HERE/expect.txt" | head -1)
mkdir -p "$HERE/runs"
bad=0
while read -r run want; do
  case "$run" in ''|'#'*) continue ;; esac
  [ $# -eq 0 ] || case " $* " in *" $run "*) ;; *) continue ;; esac
  out=$HERE/runs/$run.out
  cmd=(java -XX:+UseParallelGC -cp "$JAR" tlc2.TLC -workers 1 -metadir "$HERE/runs/states/$run"
       -config "$run.cfg" "${spec#./}")
  { echo "\$ cd $HERE && ${cmd[*]}"; (cd "$HERE" && "${cmd[@]}") 2>&1; echo "tlc exit=$?"; } >| "$out"
  # shellcheck disable=SC2086  # cols is a list of variable names
  python3 "$HERE/trace.py" "$out" $cols >| "$HERE/runs/$run.trace.txt"
  if grep -q '^Model checking completed. No error has been found' "$out"; then got=PASS
  elif grep -q '^Error: Invariant .* is violated' "$out"; then got=FAIL
  else got=ERROR; fi
  states=$(grep -oE '[0-9,]+ distinct states found' "$out" | tail -1)
  steps=$(grep -cE '^State [0-9]+:' "$out")
  mark=ok; [ "$got" = "$want" ] || { mark=UNEXPECTED; bad=1; }
  printf '%-32s %-5s (expected %-4s %s)  %s%s\n' "$run" "$got" "$want" "$mark" "${states:-no state count}" \
    "$([ "$got" = FAIL ] && echo ", trace $steps steps in runs/$run.trace.txt")"
done < "$HERE/expect.txt"
[ $bad -eq 0 ] && [ $drift -eq 1 ] && exit 2
exit $bad
