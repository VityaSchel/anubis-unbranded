#!/usr/bin/env bash
set -euo pipefail

base=$1
patch=$2
generated='*_templ.go'
reproducible_date=$(git log -1 --format=%cI "$base")

export GIT_COMMITTER_NAME="github-actions[bot]"
export GIT_COMMITTER_EMAIL="41898282+github-actions[bot]@users.noreply.github.com"
export GIT_COMMITTER_DATE=$reproducible_date

git checkout --quiet --detach "$base"
if ! git cherry-pick --no-commit "$patch" >&2; then
	mapfile -t unresolvable < <(git diff --name-only --diff-filter=U -- ":!$generated")
	if ((${#unresolvable[@]} > 0)); then
		git reset --quiet --hard
		echo "::error title=Fork commit no longer applies::Conflicts on top of $base: ${unresolvable[*]}" >&2
		exit 1
	fi
fi

git rm -r --force --quiet --ignore-unmatch -- "$generated"
git checkout "$base" -- "$generated"
go generate ./web >&2
git add --all -- "$generated"
git commit --quiet --no-verify --cleanup=verbatim --reuse-message="$patch"
git rev-parse HEAD
