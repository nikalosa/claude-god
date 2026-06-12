#!/usr/bin/env bash
# Claude Code context budget — measures always-on + per-scenario context load for the
# LIVE .claude/ layout (root CLAUDE.md, .claude/rules/*.md with/without `paths:`
# frontmatter, and nested <dir>/CLAUDE.md). Repo-agnostic.
#
# Token estimate = bytes / 4 (standard English heuristic). A routing/budget gauge,
# not an exact tokenizer — ground-truth with the real /context where possible.
#
# Usage:
#   claude-context-budget.sh            # idle + auto-discovered per-subtree scenarios
#   claude-context-budget.sh <path>...  # custom: files "touched" this session
set -euo pipefail
cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

RULES_DIR=".claude/rules"
ROOT="CLAUDE.md"

bytes() { wc -c <"$1" 2>/dev/null || echo 0; }
tok()   { echo $(( $1 / 4 )); }

# Does a rule file carry a `paths:` key inside its leading --- frontmatter block?
has_paths_frontmatter() {
  awk '
    NR==1 && $0!="---" { exit 1 }      # no frontmatter at all -> unscoped
    NR==1 { infm=1; next }
    infm && $0=="---" { exit 1 }       # closed frontmatter, never saw paths
    infm && /^paths:[[:space:]]*/ { exit 0 }
  ' "$1"
}

# Print the `paths:` globs (YAML array or inline) of a rule, one per line.
rule_globs() {
  awk '
    NR==1 && $0!="---" { exit }
    NR==1 { infm=1; next }
    infm && $0=="---" { exit }
    infm && /^paths:/ {
      inp=1
      line=$0; sub(/^paths:[[:space:]]*/,"",line)
      if (line ~ /\[/) {                # inline array: paths: ["a", "b"]
        gsub(/[\[\]"'"'"']/,"",line); n=split(line,a,","); for(i=1;i<=n;i++){gsub(/^[ ]+|[ ]+$/,"",a[i]); if(a[i]!="")print a[i]}
        exit
      }
      next
    }
    inp && /^[[:space:]]*-[[:space:]]*/ { g=$0; sub(/^[[:space:]]*-[[:space:]]*/,"",g); gsub(/["'"'"']/,"",g); print g; next }
    inp && /^[^[:space:]-]/ { exit }    # next key -> end of list
  ' "$1"
}

# Glob match via python fnmatch (handles ** reasonably for our globs).
glob_match() { python3 - "$1" "$2" <<'PY'
import sys, fnmatch
glob, path = sys.argv[1], sys.argv[2]
g = glob.replace("/**", "*").replace("**/", "*")   # flatten ** for fnmatch
print("1" if fnmatch.fnmatch(path, g) else "0")
PY
}

unscoped_rules() { for f in "$RULES_DIR"/*.md; do [ -e "$f" ] || continue; has_paths_frontmatter "$f" || echo "$f"; done; }

# ---- Idle: root + every unscoped rule (these load at launch) ----
idle_total=$(bytes "$ROOT")
idle_files=("$ROOT")
while IFS= read -r f; do [ -n "$f" ] && { idle_total=$(( idle_total + $(bytes "$f") )); idle_files+=("$f"); }; done < <(unscoped_rules)

echo "============================================================"
echo " Claude Code context budget — $(git branch --show-current 2>/dev/null || echo no-git)"
echo "============================================================"
printf "\nIDLE (always-on at launch): root + %d unscoped rule(s)\n" "$(( ${#idle_files[@]} - 1 ))"
printf "  %-40s ~%6d tok\n" "TOTAL IDLE" "$(tok $idle_total)"
if [ "$(( ${#idle_files[@]} - 1 ))" -gt 0 ]; then
  echo "  unscoped rules still loading eagerly (candidates for paths: or root):"
  for f in "${idle_files[@]:1}"; do printf "    - %-36s ~%5d tok\n" "$(basename "$f")" "$(tok $(bytes "$f"))"; done
fi

# ---- Scenario loader: given touched paths, sum root + nested CLAUDE.md + matching rules ----
scenario() {
  local name="$1"; shift
  local total=0; local -a loaded=("$ROOT"); total=$(bytes "$ROOT")
  for p in "$@"; do
    local d; d="$(dirname "$p")"
    while [ "$d" != "." ] && [ "$d" != "/" ]; do
      if [ -f "$d/CLAUDE.md" ]; then
        case " ${loaded[*]} " in *" $d/CLAUDE.md "*) :;; *) loaded+=("$d/CLAUDE.md"); total=$(( total + $(bytes "$d/CLAUDE.md") ));; esac
      fi
      d="$(dirname "$d")"
    done
    for f in "$RULES_DIR"/*.md; do
      [ -e "$f" ] || continue
      has_paths_frontmatter "$f" || continue
      case " ${loaded[*]} " in *" $f "*) continue;; esac
      while IFS= read -r g; do
        [ -n "$g" ] || continue
        if [ "$(glob_match "$g" "$p")" = "1" ]; then
          loaded+=("$f"); total=$(( total + $(bytes "$f") )); break
        fi
      done < <(rule_globs "$f")
    done
  done
  printf "  %-40s ~%6d tok  (%d files)\n" "$name" "$(tok $total)" "${#loaded[@]}"
}

# Nested CLAUDE.md subtree dirs (excluding root). Tolerant of zero matches / non-git.
nested_dirs() {
  { git ls-files 2>/dev/null || find . -path '*CLAUDE.md' 2>/dev/null; } \
    | grep -E '(^|/)CLAUDE\.md$' | grep -vx 'CLAUDE.md' | grep -vx './CLAUDE.md' \
    | grep -v '/node_modules/' | sed 's#/CLAUDE\.md$##; s#^\./##' | sort -u || true
}

# A representative real file inside a subtree (so glob-scoped rules trigger).
sample_file() {
  { git ls-files "$1" 2>/dev/null || find "$1" -type f 2>/dev/null; } \
    | grep -v -E '(^|/)CLAUDE\.md$' | grep -v '/node_modules/' | sed -n '1p' || true
}

echo ""
if [ "$#" -gt 0 ]; then
  echo "CUSTOM scenario (touched: $*)"
  scenario "custom" "$@"
else
  echo "STANDARD scenarios (auto-discovered, one per nested CLAUDE.md subtree):"
  found=0
  while IFS= read -r d; do
    [ -n "$d" ] || continue
    s="$(sample_file "$d")"; [ -n "$s" ] || s="$d/__file__"
    scenario "touch $d" "$s"; found=1
  done < <(nested_dirs)
  [ "$found" -eq 1 ] || echo "  (no nested CLAUDE.md found — idle above is the whole story)"
fi
echo ""
