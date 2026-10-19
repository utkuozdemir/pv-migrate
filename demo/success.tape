# A migration between two claims, over the strategy most people end up on.
#
# The settings come first: the recorder ignores any Set directive that is
# not at the top of the file, and says so in a warning that is easy to miss.
Source demo/common.tape

Output img/demo.gif

# A prompt of one plain character. An escape sequence in it would be counted as
# visible width by the shell's line editor, which then wraps the typed command
# in the wrong column. The continuation prompt is two spaces, so the command
# reads as it would in the docs, one group of flags per line. The binary comes
# from the PATH that task demo:record sets.
Hide
Type `PS1='❯ ' PS2='  '`
Enter
Type "clear"
Enter
Show

Sleep 1s
Type "pv-migrate --source-namespace demo --source app-data \"
Enter
Type "--dest-namespace demo --dest app-data-new \"
Enter
Type "--strategies clusterip"
Sleep 500ms
Enter

# Waiting for the prompt to come back rather than for a phrase in the output.
# The phrase can scroll off screen before the command exits, and a tape that
# stops early kills the migration and leaves its resources on the cluster.
Wait+Line@5m /^\x{276f}\s*$/
Sleep 4s
