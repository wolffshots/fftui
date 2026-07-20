# demo.tape — VHS script for the fftui walkthrough GIF.
#
# Render locally with:  go build -o fftui . && vhs demo.tape
# CI renders it via charmbracelet/vhs-action (see .github/workflows/demo.yml).

Output demo.gif

Set Shell "bash"
Set FontSize 15
Set Width 1360
Set Height 720
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms

Type "./fftui --csv testdata/cycles.csv" Enter
Sleep 2.5s

# Cycles table — browse, sort, then filter
Down 4
Sleep 1.2s
Type "s"
Sleep 900ms
Type "s"
Sleep 900ms
Type "/"
Type "FX001"
Sleep 1.2s
Enter
Sleep 1.5s
Escape
Sleep 900ms

# Open a cycle's detail, then back to the table
Down 2
Enter
Sleep 2.5s
Type "q"
Sleep 900ms

# Analytics — cycle granularity (Tab) and toggle dead buckets (a)
Type "2"
Sleep 2s
Tab
Sleep 1.3s
Tab
Sleep 1.3s
Type "a"
Sleep 2s

# Detail view
Type "3"
Sleep 2.5s

# Charts
Type "4"
Sleep 2.5s

# Live view (CSV mode shows a hint)
Type "5"
Sleep 2s

# Back to the table, then quit
Type "1"
Sleep 1.2s
Type "q"
Sleep 800ms
