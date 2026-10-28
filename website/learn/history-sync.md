<!-- toc -->

Elvish can keep command history in step across machines. Every machine holds a
full copy; there is no server. One always-on machine, the **hub**, is the
remote every other machine syncs with, and it relays their histories to each
other. Syncing is `elvish -sync <hub>`, which runs `elvish -relay` on the hub
over ssh and exchanges whatever each side is missing. See the
[command reference](../ref/command.html#sync-flags) for the flags.

Each step below is given plain and for Nix. The hub is assumed to run NixOS and
the clients home-manager; the plain form works anywhere. Replace `eric`,
`hub.example.net` and the key.

# A key for syncing, on each client

One passwordless key per client, used for nothing else. Private keys are not
Nix material; generate it by hand:

```sh
ssh-keygen -t ed25519 -N '' -f ~/.ssh/elvish-sync -C elvish-sync
```

# The key on the hub, restricted to the relay

`restrict` turns off pty, port forwarding, agent forwarding and X11; the forced
command means the key can relay records and nothing else, whatever the client
asks to run. Use the full path to the binary: non-interactive ssh sessions have
a minimal PATH.

Using your own account keeps the hub's interactive history and the relay in one
store. A dedicated user is optional hardening for a hub other people also use;
the forced command still belongs on the key, not on the login shell.

Plain, in `~/.ssh/authorized_keys` on the hub:

```
restrict,command="/usr/local/bin/elvish -relay" ssh-ed25519 AAAA... elvish-sync
```

NixOS, in the hub's configuration (NixOS rewrites the line, path included, on
every rebuild):

```nix
{ pkgs, ... }: {
  services.openssh.enable = true;
  users.users.eric.openssh.authorizedKeys.keys = [
    ''restrict,command="${pkgs.elvish}/bin/elvish -relay" ssh-ed25519 AAAA... elvish-sync''
  ];
}
```

The relay finds the storage daemon under `$XDG_RUNTIME_DIR`, which logind sets
for ssh sessions on NixOS and on any distribution running pam_systemd.

# An ssh alias for the hub, on each client

The alias is what `elvish -sync` is given. It must be a separate alias from the
one used for interactive logins: ssh offers the keys of the matching block, and
the restricted key would turn an interactive login into a relay.

Plain, in `~/.ssh/config`:

```
Host elvish-hub
    HostName hub.example.net
    User eric
    IdentityFile ~/.ssh/elvish-sync
    IdentitiesOnly yes
```

home-manager:

```nix
programs.ssh.matchBlocks.elvish-hub = {
  hostname = "hub.example.net";
  user = "eric";
  identityFile = "~/.ssh/elvish-sync";
  identitiesOnly = true;
};
```

Then accept the hub's host key once, since `-sync` runs ssh in batch mode and
never prompts. With a forced command any command is replaced by the relay, so
give it an empty stdin and it exits at once:

```sh
ssh elvish-hub < /dev/null
```

On NixOS the host key can be pinned instead with `programs.ssh.knownHosts` in
the client's system configuration.

# First sync, by hand

```sh
elvish -sync elvish-hub
```

Prints `pulled N, pushed M`. Both storage daemons start if they are not running
and exit afterwards. If the sync reports that this machine's series has
diverged, which happens when the database was deleted but the host ID beside it
kept, run `elvish -sync -reset elvish-hub` once; nothing is lost.

# A timer, on each client

The service needs `ssh` on its PATH; systemd user services do not get the login
shell's PATH.

Plain, `~/.config/systemd/user/elvish-sync.service`:

```ini
[Unit]
Description=Sync elvish history with the hub

[Service]
Type=oneshot
ExecStart=/usr/local/bin/elvish -sync elvish-hub
Environment=PATH=/usr/bin:/bin
```

and `~/.config/systemd/user/elvish-sync.timer`:

```ini
[Unit]
Description=Sync elvish history every five minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min

[Install]
WantedBy=timers.target
```

then `systemctl --user enable --now elvish-sync.timer`.

home-manager:

```nix
{ pkgs, ... }: {
  systemd.user.services.elvish-sync = {
    Unit.Description = "Sync elvish history with the hub";
    Service = {
      Type = "oneshot";
      ExecStart = "${pkgs.elvish}/bin/elvish -sync elvish-hub";
      Environment = [ "PATH=${pkgs.lib.makeBinPath [ pkgs.openssh ]}" ];
    };
  };
  systemd.user.timers.elvish-sync = {
    Unit.Description = "Sync elvish history every five minutes";
    Timer = { OnBootSec = "2min"; OnUnitActiveSec = "5min"; };
    Install.WantedBy = [ "timers.target" ];
  };
}
```

A failed run (hub down, laptop offline) exits non-zero and the next tick tries
again; nothing is kept between runs, so there is nothing to clean up.

# Open shells

A shell's view of the history is frozen at the point it started, so entries
pulled by a sync are invisible to it until
[`edit:history:fast-forward`](../ref/edit.html#edit:history:fast-forward). The
sync writes the current time to `sync.stamp` beside the database after every
pull. A prompt hook in `rc.elv` that re-reads the file and fast-forwards when it
changed costs one small read per prompt:

```elvish
use os
var sync-stamp = (if (has-env XDG_STATE_HOME) { put $E:XDG_STATE_HOME } else { put ~/.local/state })/elvish/sync.stamp
var sync-seen = (if (os:exists $sync-stamp) { slurp < $sync-stamp } else { put '' })
set edit:before-readline = [$@edit:before-readline {
  if (os:exists $sync-stamp) {
    var now = (slurp < $sync-stamp)
    if (not-eq $now $sync-seen) {
      set sync-seen = $now
      edit:history:fast-forward
    }
  }
}]
```

Pulled entries appear as the most recent ones in the editor, since it orders by
the sequence they were added locally.
