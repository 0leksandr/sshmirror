A fast, one-direction SSH synchronization client.

Basic usage scenario - local development with constant uploading to remote server via SSH. So basically, a fast alternative to SFTP manager of Jetbrains IDEs.

Example usage:

```shell script
sshstream \
    -i=~/.ssh/my_rsa \
    -t=5 \
    -ignored='(^\.git/|^\.idea/|~$)' \
    ~/myProject me@remote.server /var/www/html/myProject
```
