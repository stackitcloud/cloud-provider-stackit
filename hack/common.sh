#!/usr/bin/env bash

# See https://misc.flogisoft.com/bash/tip_colors_and_formatting

color-green() { # Green
  echo -e "\x1B[1;32m${@}\x1B[0m"
}

color-step() { # Yellow
  echo -e "\x1B[1;33m${@}\x1B[0m"
}

color-context() { # Bold blue
  echo -e "\x1B[1;34m${@}\x1B[0m"
}

color-missing() { # Yellow
  echo -e "\x1B[1;33m${@}\x1B[0m"
}

prepare_token() {
  local namespace="$1"
  local name="$2"
  local rolekind="$(echo "$3" | tr '[:upper:]' '[:lower:]')"
  local role="$4"
  local duration="${5:-10m}"

  result="$(kubectl -n "$namespace" create serviceaccount "$name" 2>&1)"
  # ignore "already exists" errors
  if ! [[ "$result" == *"created"* ]] && ! [[ "$result" == *"already exists"* ]] ; then
    echo "$result"
    return 1
  fi
  result="$(kubectl -n "$namespace" create "${rolekind}binding" "$name" --"$rolekind" "$role" --serviceaccount "$namespace:$name" 2>&1)"
  # ignore "already exists" errors
  if ! [[ "$result" == *"created"* ]] && ! [[ "$result" == *"already exists"* ]] ; then
    echo "$result"
    return 1
  fi

  kubectl -n "$namespace" create token "$name" --duration="$duration"
}

prepare_access() {
  local namespace="$1"
  local name="$2"
  local rolekind="$(echo "$3" | tr '[:upper:]' '[:lower:]')"
  local role="$4"
  local tmp_kubeconfig="$5"

  color-step "Preparing access for $name"

  local token
  token="$(prepare_token "$namespace" "$name" "$rolekind" "$role")"

  # create temporary kubeconfig clone that we can modify
  kubectl config view --minify --raw >"$tmp_kubeconfig"

  # set up kubeconfig with ServiceAccount token and context that the tool expects
  (
    export KUBECONFIG="$tmp_kubeconfig"
    kubectl config set-credentials token --token="$token"
    kubectl config set-context --current --user token

    kubectl config rename-context "$(kubectl config current-context)" ske-prow-build
  )

  color-green "done"
}

cleanup_access() {
  local namespace="$1"
  local name="$2"
  local rolekind="$(echo "$3" | tr '[:upper:]' '[:lower:]')"
  local role="$4"
  local tmp_kubeconfig="$5"

  color-step "Cleaning up cluster access for $name"

  rm "$tmp_kubeconfig"
  kubectl -n "$namespace" delete --ignore-not-found "${rolekind}binding" "$name"
  kubectl -n "$namespace" delete --ignore-not-found serviceaccount "$name"

  color-green "done"
}

# Collect_reports copies junit.xml reports from the individual package directories to the given directory.
# The result is a flat target directory, where the file names indicate the original package path.
function collect_reports {
  local junit_dir="$1"

  while read -r report; do
    dest="${report#./}"
    dest="${dest/\/junit.xml/.xml}"
    dest="$(echo "$dest" | tr "/" "_")"
    cp "$report" "$junit_dir/$dest"
  done < <(find . -name junit.xml)
}
