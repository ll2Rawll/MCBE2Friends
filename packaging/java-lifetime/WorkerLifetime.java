import java.util.concurrent.TimeUnit;

/** macOS has no parent-death signal. Tie the game JVM to its licensed worker. */
public final class WorkerLifetime {
    private WorkerLifetime() {}

    public static void premain(String expectedPid) {
        long pid = Long.parseLong(expectedPid);
        ProcessHandle parent = ProcessHandle.current().parent().orElse(null);
        if (parent == null || parent.pid() != pid) {
            Runtime.getRuntime().halt(1);
        }
        parent.onExit().thenRun(() -> {
            Thread deadline = new Thread(() -> {
                try {
                    TimeUnit.SECONDS.sleep(45);
                } catch (InterruptedException ignored) {
                    Thread.currentThread().interrupt();
                }
                Runtime.getRuntime().halt(1);
            }, "Mcbe2Friends shutdown deadline");
            deadline.setDaemon(true);
            deadline.start();
            System.exit(0);
        });
    }
}
