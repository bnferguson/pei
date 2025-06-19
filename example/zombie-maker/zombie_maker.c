#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <sys/types.h>
#include <signal.h>
#include <time.h>

// Function to create a zombie process
void create_zombie() {
    pid_t pid = fork();
    if (pid < 0) {
        perror("fork failed");
        exit(1);
    }

    if (pid > 0) {
        // Parent process
        printf("Parent process (PID: %d) created child (PID: %d)\n", getpid(), pid);
        // Exit parent immediately, creating a zombie
        exit(0);
    }

    // Child process
    printf("Child process (PID: %d) started\n", getpid());
    // Child keeps running for a while
    sleep(60);
    printf("Child process (PID: %d) exiting\n", getpid());
    exit(0);
}

int main() {
    printf("Zombie maker service started (PID: %d)\n", getpid());

    // Create zombies periodically
    while (1) {
        create_zombie();
        // Wait a bit before creating the next zombie
        sleep(30);
    }

    return 0;
}