# A migration between two claims, over the strategy most people end up on.
Output img/demo.gif

Source demo/common.tape

# A prompt of one plain character. An escape sequence in it would be counted as
# visible width by the shell's line editor, which then wraps the typed command
# in the wrong column. The binary comes from the PATH that task demo:record sets.
Hide
Type `PS1='❯ '`
Enter
Type "clear"
Enter
Show

Sleep 1s
Type "pv-migrate -n demo --source app-data -N demo --dest app-data-new -s clusterip"
Sleep 500ms
Enter

# Waiting for the prompt to come back rather than for a phrase in the output.
# The phrase can scroll off screen before the command exits, and a tape that
# stops early kills the migration and leaves its resources on the cluster.
Wait+Line@5m /^\x{276f}\s*$/
Sleep 4s
