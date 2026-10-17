/* Linux-only benchmark helper. Not included in the router package. */
#define _GNU_SOURCE
#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/resource.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
static volatile sig_atomic_t child_pid = 0;
static void forward_signal(int sig) { if (child_pid > 0) kill(child_pid, sig); }
int main(int argc, char **argv) {
    if (argc < 2) return 2;
    struct sigaction action = {0};
    action.sa_handler = forward_signal;
    sigemptyset(&action.sa_mask);
    sigaction(SIGTERM, &action, NULL);
    sigaction(SIGINT, &action, NULL);
    pid_t pid = fork();
    if (pid < 0) { perror("fork"); return 1; }
    if (pid == 0) {
        dup2(STDERR_FILENO, STDOUT_FILENO);
        execvp(argv[1], argv + 1);
        _exit(127);
    }
    child_pid = pid;
    int status;
    struct rusage usage = {0};
    while (wait4(pid, &status, 0, &usage) < 0) {
        if (errno != EINTR) { perror("wait4"); return 1; }
    }
    printf("%ld\n", usage.ru_maxrss);
    return WIFEXITED(status) && WEXITSTATUS(status) == 127 ? 1 : 0;
}
