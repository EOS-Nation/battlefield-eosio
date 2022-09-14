#!/bin/bash

set -e

ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

nodeos_pid=""
current_dir=`pwd`
DEEP_MIND=true

function cleanup {
    if [[ $nodeos_pid != "" ]]; then
      echo "Closing nodeos process"
      kill -s TERM $nodeos_pid &> /dev/null || true
    fi

    cd $current_dir
    exit 0
}

function main() {
  target="$1"
  eos_bin="$2"

  if [[ ! -d "$ROOT/$target" ]]; then
    echo "The target directory '$ROOT/$target' does not exist, check first provided argument."
    exit 1
  fi

  if [[ ! -f $eos_bin ]]; then
    echo "The 'nodeos' binary received does not exist, check second provided argument."
    exit 1
  fi

  # Trap exit signal and clean up
  trap cleanup EXIT

  pushd $ROOT &> /dev/null

  deep_mind_log_file="./$target/deep-mind.dmlog"
  nodeos_log_file="./$target/nodeos.log"
  eosc_boot_log_file="eosc-boot.log"

  rm -rf "$ROOT/$target/blocks/" "$ROOT/$target/state/"

  extra_args=
  if [[ $DEEP_MIND == "true" ]]; then
    extra_args="--deep-mind"
  fi

  echo "Command: $eos_bin $extra_args --data-dir="$ROOT/$target" --config-dir="$ROOT/$target" --genesis-json="$ROOT/$target/genesis.json" 1> $deep_mind_log_file 2> $nodeos_log_file"
  ($eos_bin $extra_args --data-dir="$ROOT/$target" --config-dir="$ROOT/$target" --genesis-json="$ROOT/$target/genesis.json" 1> $deep_mind_log_file 2> $nodeos_log_file) &
  nodeos_pid=$!
  echo "Started $1 node at PID $nodeos_pid"

  export EOSC_GLOBAL_INSECURE_VAULT_PASSPHRASE=secure
  export EOSC_GLOBAL_API_URL=http://localhost:9898
  export EOSC_GLOBAL_VAULT_FILE="$ROOT/eosc-vault.json"

  echo "Booting $1 node with smart contracts ..."
  pushd $target
  eosc boot ../bootseq.yaml --reuse-genesis --api-url http://localhost:9898 #1> /dev/null
  mv output.log ${eosc_boot_log_file}
  popd 1> /dev/null

  echo "Booting completed, launching test cases..."
  eosc system updateauth battlefield1 active owner "$ROOT"/perms/battlefield1_active_auth.yaml
  set +e

  eosc tx create battlefield1 dtrx '{"account": "battlefield1", "fail_now": false, "fail_later": false, "fail_later_nested": false, "delay_sec": 1, "nonce": "1"}' -p battlefield1
  sleep 0.6
  eosc tx create battlefield1 dtrx '{"account": "battlefield1", "fail_now": false, "fail_later": true, "fail_later_nested": false, "delay_sec": 1, "nonce": "1"}' -p battlefield1

  if [[ "$?" != "0" ]]; then
    echo "Test transactions failed"
    exit 1
  fi
  set -e


  # Kill `nodeos` process
  echo ""
  echo "Exiting in 2 secs"
  sleep 2

  if [[ $nodeos_pid != "" ]]; then
    kill -s TERM $nodeos_pid &> /dev/null || true
    sleep 0.5
  fi

  if [[ $DEEP_MIND == "true" ]]; then
    # Print Deep Mind Statistics
    set +ex
    echo "Statistics"
    echo " Blocks: `cat "$deep_mind_log_file" | grep "ACCEPTED_BLOCK" | wc -l | tr -d ' '`"
    echo " Transactions: `cat "$deep_mind_log_file" | grep "APPLIED_TRANSACTION" | wc -l | tr -d ' '`"
    echo ""
    echo " Creation Op: `cat "$deep_mind_log_file" | grep "CREATION_OP" | wc -l | tr -d ' '`"
    echo " Database Op: `cat "$deep_mind_log_file" | grep "DB_OP" | wc -l | tr -d ' '`"
    echo " Deferred Transaction Op: `cat "$deep_mind_log_file" | grep "DTRX_OP" | wc -l | tr -d ' '`"
    echo " Feature Op: `cat "$deep_mind_log_file" | grep "FEATURE_OP" | wc -l | tr -d ' '`"
    echo " Permission Op: `cat "$deep_mind_log_file" | grep "PERM_OP" | wc -l | tr -d ' '`"
    echo " Resource Limits Op: `cat "$deep_mind_log_file" | grep "RLIMIT_OP" | wc -l | tr -d ' '`"
    echo " RAM Op: `cat "$deep_mind_log_file" | grep "RAM_OP" | wc -l | tr -d ' '`"
    echo " RAM Correction Op: `cat "$deep_mind_log_file" | grep "RAM_CORRECTION_OP" | wc -l | tr -d ' '`"
    echo " Table Op: `cat "$deep_mind_log_file" | grep "TBL_OP" | wc -l | tr -d ' '`"
    echo " Transaction Op: `cat "$deep_mind_log_file" | grep "TRX_OP" | wc -l | tr -d ' '`"
    echo ""
  fi

  echo "Inspect log files"
  if [[ $DEEP_MIND == "true" ]]; then
    echo " Deep Mind logs: cat $deep_mind_log_file"
  fi
  echo " Nodeos logs: cat $nodeos_log_file"
  echo " eosc boot logs: cat $target/$eosc_boot_log_file"
  echo ""
}


main $@
