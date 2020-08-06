A fast, real-time, continuous, one-directional (local⇒remove) SSH filesystem synchronization client.

Basic usage scenario - local development with constant uploading to remote server via SSH. So basically, a fast alternative to SFTP manager of Jetbrains IDEs.

Example usage:

- start program:
  ```shell script
  sshstream \
      -i=~/.ssh/my_rsa \
      -e='(^\.git/|^\.idea/|~$)' \
      ~/myProject me@remote.server /var/www/html/myProject
  ```
- make some changes to files in your local directory (create/edit/move/delete)
- see them being reflected on remote server

Proc:
- speed (especially on slow connections). See sample benchmark:
  - switching between `git` branches with total difference of 30 files (550Kb)
    - PhpStorm: 1.5min
    - `sshstream`: 3sec

Cons:
- one-directional. So, to check remote files, you'll have to use other tools
- no visible speed-up on frequent uploading of singular files (speed-up is ≈10%)

Features:
- using `rsync` for transferring files
- transferring in batches instead of one-by-one. General rules for grouping files in queue into a batch are:
  - last modification was made 0.5sec ago
  - first modification was made 5sec ago, and since then new modifications occur constantly (without break for 0.5sec)
- using `ssh` "Master connection" feature to keep one constant connection. Thus, once-in-a-while uploads do not need to establish connection over again
- FS modifications tracked by `fsnotify`. Credits: https://github.com/fsnotify/fsnotify

Please submit bugs reports to https://github.com/0leksandr/sshstream/issues
