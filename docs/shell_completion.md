# Shell Completion

Lightning Loop ships with built-in shell completion for the `loop` CLI. This
streamlines working with `loop` command and flags across supported shells such
as `bash`, `zsh`, `fish`, and `powershell`.

## Enabling Bash Completion

1. Create the completions directory if it does not exist.
   ```bash
   mkdir -p ~/.local/share/bash-completion/completions/
   ```
2. Generate the completion script for `loop` and save it.
   ```bash
   loop completion bash > ~/.local/share/bash-completion/completions/loop
   ```
3. Source the script in the current shell and add it to your shell profile so it
   loads automatically.
   ```bash
   source ~/.local/share/bash-completion/completions/loop
   echo 'source ~/.local/share/bash-completion/completions/loop' >> ~/.bashrc
   ```

After enabling completion you can press `TAB` to see available commands and
flags:

```bash
$ loop static <TAB>
help             listswaps        new
in               listunspent      summary
listdeposits     listwithdrawals  withdraw

$ loop static in --<TAB>
--all              --label            --payment_timeout  --utxo
--amount           --last_hop         --route_hints
```

## Other Shells

Shell completion is also available for `zsh`, `fish`, and `powershell`. Replace
`bash` in the command shown above with your shell name and adjust the output
path so that the file is sourced by your shell.

Example of completion in `fish`:

```
> loop static <TAB>
help                          (Shows a list of commands or help for one command)
in                                 (Loop in funds from static address deposits.)
listdeposits  (Displays static address deposits. A filter can be applied to on…)
listswaps                      (Shows a list of finalized static address swaps.)
listunspent                               (List unspent static address outputs.)
listwithdrawals                         (Display a summary of past withdrawals.)
new                                       (Create a new static loop in address.)
summary               (Display a summary of static address related information.)
withdraw                                (Withdraw from static address deposits.)
```
