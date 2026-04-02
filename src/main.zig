const std = @import("std");
const yaml = @import("yaml.zig");
const kernel = @import("kernel.zig");

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    // 1. Read config
    const config_path = "tabula.yaml";
    const config = yaml.parse(allocator, config_path) catch |err| {
        std.debug.print("error: cannot read {s}: {}\n", .{ config_path, err });
        std.process.exit(1);
    };
    defer config.deinit(allocator);

    // 2. Boot kernel
    var k = kernel.Kernel.init(allocator);
    defer k.deinit();
    k.installSignalHandlers();

    // 3. Spawn LLM skill (inherits env from parent process)
    const llm_cmd = config.llm_skill orelse {
        std.debug.print("error: tabula.yaml must specify llm\n", .{});
        std.process.exit(1);
    };

    std.debug.print("[main] LLM skill: {s}\n", .{llm_cmd});

    const llm_pid = k.spawn(llm_cmd, null) catch |err| {
        std.debug.print("error: cannot spawn LLM skill '{s}': {}\n", .{ llm_cmd, err });
        std.process.exit(1);
    };
    std.debug.print("[main] LLM spawned as PID {d}\n", .{llm_pid});

    // 4. Send genesis prompt to LLM
    const genesis = config.genesis orelse {
        std.debug.print("error: tabula.yaml must specify genesis\n", .{});
        std.process.exit(1);
    };

    std.debug.print("[main] genesis length: {d} bytes\n", .{genesis.len});
    std.debug.print("[main] genesis:\n{s}\n---\n", .{genesis});

    // Send kernel tools definition as first line (embedded JSON)
    {
        const writer = if (k.processes[llm_pid]) |*proc| (if (proc.child.stdin) |s| s.writer() else null) else null;
        if (writer) |w| {
            const tools_json = @embedFile("kernel.tools.json");
            // Send as single line: strip newlines
            for (tools_json) |c| {
                if (c != '\n' and c != '\r') {
                    try w.writeByte(c);
                }
            }
            try w.writeAll("\n");

            // System prompt: kernel description (hardcoded) + genesis (user)
            try w.writeAll("You have kernel tools: SPAWN, EXEC, KILL, SEND. Use them via tool calls.\n");
            try w.writeAll("SPAWN starts a long-running process (returns PID, stdout auto-piped to you). EXEC runs a command synchronously (returns stdout). KILL stops a process. SEND writes to a process stdin.\n");

            // Genesis as user-defined behavior
            var line_iter = std.mem.splitScalar(u8, genesis, '\n');
            while (line_iter.next()) |line| {
                const trimmed = std.mem.trim(u8, line, " \t\r");
                if (trimmed.len == 0) {
                    try w.writeAll(" \n");
                } else {
                    try w.writeAll(line);
                    try w.writeAll("\n");
                }
            }
            try w.writeAll("\n"); // empty line = end of system prompt
        } else {
            std.debug.print("error: cannot write to LLM stdin\n", .{});
            std.process.exit(1);
        }
    }

    std.debug.print("[main] genesis sent, entering main loop\n", .{});

    // 5. Main loop: read LLM output, execute commands
    k.run(llm_pid) catch |err| {
        std.debug.print("error: kernel loop failed: {}\n", .{err});
        std.process.exit(1);
    };
}
