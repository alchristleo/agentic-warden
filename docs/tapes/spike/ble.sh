# Spike (a): load ble.sh into the recording shell. BLE_DIR points at the unpacked release.
PS1='\[\e[34m\]\W\[\e[0m\] \[\e[32m\]\$\[\e[0m\] '
source "$BLE_DIR/ble.sh" --attach=prompt --norc
bleopt prompt_eol_mark=''
