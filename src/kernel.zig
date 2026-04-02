const std = @import("std");
const posix = std.posix;

const max_processes = 256;

const Process = struct {
    child: std.process.Child,
    argv: []const []const u8,
    alive: bool,
};

/// Global pointer for signal handler to reach the kernel.
var global_kernel: ?*Kernel = null;

fn handleSignal(_: c_int) callconv(.C) void {
    if (global_kernel) |k| {
        k.killAll();
    }
    std.process.exit(0);
}

pub const Kernel = struct {
    allocator: std.mem.Allocator,
    processes: [max_processes]?Process,

    pub fn init(allocator: std.mem.Allocator) Kernel {
        return Kernel{
            .allocator = allocator,
            .processes = [_]?Process{null} ** max_processes,
        };
    }

    /// Register signal handlers. Must be called after Kernel is at its final address.
    pub fn installSignalHandlers(self: *Kernel) void {
        global_kernel = self;
        const act = posix.Sigaction{
            .handler = .{ .handler = handleSignal },
            .mask = posix.empty_sigset,
            .flags = 0,
        };
        posix.sigaction(posix.SIG.INT, &act, null);
        posix.sigaction(posix.SIG.TERM, &act, null);
        posix.sigaction(posix.SIG.HUP, &act, null);
    }

    pub fn deinit(self: *Kernel) void {
        self.killAll();
        global_kernel = null;
    }

    /// Kill all living processes and free resources.
    pub fn killAll(self: *Kernel) void {
        for (&self.processes) |*slot| {
            if (slot.*) |*proc| {
                if (proc.alive) {
                    _ = proc.child.kill() catch {};
                    _ = proc.child.wait() catch {};
                }
                self.allocator.free(proc.argv);
                slot.* = null;
            }
        }
    }

    /// Parse command string into argv, handling single/double quotes.
    /// Caller owns returned slice.
    fn parseArgv(self: *Kernel, command: []const u8) ![]const []const u8 {
        var argv_list = std.ArrayList([]const u8).init(self.allocator);
        defer argv_list.deinit();

        var i: usize = 0;
        while (i < command.len) {
            while (i < command.len and command[i] == ' ') : (i += 1) {}
            if (i >= command.len) break;

            if (command[i] == '\'' or command[i] == '"') {
                const quote = command[i];
                i += 1;
                const start = i;
                while (i < command.len and command[i] != quote) : (i += 1) {}
                try argv_list.append(command[start..i]);
                if (i < command.len) i += 1;
            } else {
                const start = i;
                while (i < command.len and command[i] != ' ') : (i += 1) {}
                try argv_list.append(command[start..i]);
            }
        }
        if (argv_list.items.len == 0) return error.EmptyCommand;

        return try self.allocator.dupe([]const u8, argv_list.items);
    }

    /// Find first free PID slot (1-based).
    fn findFreePid(self: *Kernel) !u32 {
        for (1..max_processes) |i| {
            if (self.processes[i] == null) return @intCast(i);
        }
        return error.TooManyProcesses;
    }

    /// Spawn a long-running process. Returns pid (1-based).
    pub fn spawn(self: *Kernel, command: []const u8, env: ?*const std.process.EnvMap) !u32 {
        const pid = try self.findFreePid();
        const argv = try self.parseArgv(command);

        var child = std.process.Child.init(argv, self.allocator);
        child.stdin_behavior = .Pipe;
        child.stdout_behavior = .Pipe;
        child.stderr_behavior = .Inherit;

        if (env) |e| {
            child.env_map = @constCast(e);
        }

        try child.spawn();

        self.processes[pid] = Process{
            .child = child,
            .argv = argv,
            .alive = true,
        };

        return pid;
    }

    /// Kill a process by pid.
    pub fn kill(self: *Kernel, pid: u32) !void {
        if (pid >= max_processes) return error.InvalidPid;
        if (self.processes[pid]) |*proc| {
            if (proc.alive) {
                _ = proc.child.kill() catch {};
                _ = proc.child.wait() catch {};
            }
            self.allocator.free(proc.argv);
            self.processes[pid] = null;
        } else {
            return error.InvalidPid;
        }
    }

    /// Send data to a process stdin.
    pub fn send(self: *Kernel, pid: u32, data: []const u8) !void {
        if (pid >= max_processes) return error.InvalidPid;
        if (self.processes[pid]) |*proc| {
            if (!proc.alive) return error.ProcessDead;
            const stdin = proc.child.stdin orelse return error.NoStdin;
            var writer = stdin.writer();
            try writer.writeAll(data);
            try writer.writeAll("\n");
        } else {
            return error.InvalidPid;
        }
    }

    /// Send data with \n escape decoding directly to process stdin.
    fn sendDecoded(self: *Kernel, pid: u32, raw_data: []const u8) !void {
        if (pid >= max_processes) return error.InvalidPid;
        if (self.processes[pid]) |*proc| {
            if (!proc.alive) return error.ProcessDead;
            const stdin = proc.child.stdin orelse return error.NoStdin;
            var writer = stdin.writer();

            var i: usize = 0;
            while (i < raw_data.len) {
                if (i + 1 < raw_data.len and raw_data[i] == '\\' and raw_data[i + 1] == 'n') {
                    try writer.writeByte('\n');
                    i += 2;
                } else {
                    try writer.writeByte(raw_data[i]);
                    i += 1;
                }
            }
            try writer.writeByte('\n');
        } else {
            return error.InvalidPid;
        }
    }

    /// Run a command synchronously, return stdout (+ stderr on error).
    pub fn exec(self: *Kernel, command: []const u8) ![]const u8 {
        const argv = try self.parseArgv(command);
        defer self.allocator.free(argv);

        var child = std.process.Child.init(argv, self.allocator);
        child.stdin_behavior = .Close;
        child.stdout_behavior = .Pipe;
        child.stderr_behavior = .Pipe;

        try child.spawn();

        // Read all stdout
        var output = std.ArrayList(u8).init(self.allocator);
        defer output.deinit();

        const stdout = child.stdout orelse return error.NoStdout;
        var buf: [4096]u8 = undefined;
        while (true) {
            const n = stdout.read(&buf) catch break;
            if (n == 0) break;
            try output.appendSlice(buf[0..n]);
        }

        // Read stderr
        var err_output = std.ArrayList(u8).init(self.allocator);
        defer err_output.deinit();

        if (child.stderr) |stderr| {
            while (true) {
                const n = stderr.read(&buf) catch break;
                if (n == 0) break;
                try err_output.appendSlice(buf[0..n]);
            }
        }

        _ = try child.wait();

        // Append stderr if stdout is empty
        if (output.items.len == 0 and err_output.items.len > 0) {
            try output.appendSlice("ERROR: ");
            try output.appendSlice(err_output.items);
        }

        // Replace empty lines with single space (protocol-safe)
        var safe = std.ArrayList(u8).init(self.allocator);
        defer safe.deinit();

        var line_iter = std.mem.splitScalar(u8, output.items, '\n');
        var first = true;
        while (line_iter.next()) |line| {
            if (!first) try safe.append('\n');
            first = false;
            const t = std.mem.trim(u8, line, " \t\r");
            if (t.len == 0) {
                try safe.append(' ');
            } else {
                try safe.appendSlice(line);
            }
        }

        const result = std.mem.trim(u8, safe.items, " \t\r\n");
        if (result.len == 0) return try self.allocator.dupe(u8, "OK");
        return try self.allocator.dupe(u8, result);
    }

    /// Main loop: read LLM stdout, batch-execute commands, send results back.
    pub fn run(self: *Kernel, llm_pid: u32) !void {
        if (llm_pid >= max_processes) return error.InvalidPid;
        const llm_proc = &(self.processes[llm_pid] orelse return error.InvalidPid);
        const llm_stdout = llm_proc.child.stdout orelse return error.NoStdout;

        var buf: [4096]u8 = undefined;
        var line_buf = std.ArrayList(u8).init(self.allocator);
        defer line_buf.deinit();

        var cmd_lines = std.ArrayList([]const u8).init(self.allocator);
        defer cmd_lines.deinit();

        while (true) {
            const n = llm_stdout.read(&buf) catch break;
            if (n == 0) break;

            for (buf[0..n]) |byte| {
                if (byte == '\n') {
                    const trimmed = std.mem.trim(u8, line_buf.items, " \t\r");

                    if (trimmed.len == 0) {
                        // Empty line = end of LLM turn. Execute all collected commands.
                        if (cmd_lines.items.len > 0) {
                            var results = std.ArrayList(u8).init(self.allocator);
                            defer results.deinit();

                            for (cmd_lines.items) |cmd_line| {
                                const result = self.executeCommand(cmd_line, llm_pid) catch |err| {
                                    std.debug.print("command error: {}\n", .{err});
                                    continue;
                                };
                                if (result) |r| {
                                    try results.appendSlice(r);
                                    try results.append('\n');
                                    self.allocator.free(r);
                                }
                            }

                            if (results.items.len > 0) {
                                try self.send(llm_pid, results.items[0 .. results.items.len - 1]);
                                try self.send(llm_pid, "");
                            }

                            for (cmd_lines.items) |cmd_line| {
                                self.allocator.free(cmd_line);
                            }
                            cmd_lines.clearRetainingCapacity();
                        }
                    } else {
                        const duped = try self.allocator.dupe(u8, trimmed);
                        try cmd_lines.append(duped);
                        std.debug.print("[kernel] LLM: {s}\n", .{trimmed});
                    }

                    line_buf.clearRetainingCapacity();
                } else {
                    try line_buf.append(byte);
                }
            }
        }
    }

    /// Execute a single command. Returns result string (caller owns) or null.
    fn executeCommand(self: *Kernel, line: []const u8, llm_pid: u32) !?[]const u8 {
        if (std.mem.startsWith(u8, line, "SPAWN ")) {
            const cmd = std.mem.trim(u8, line["SPAWN ".len..], " \t");
            const pid = try self.spawn(cmd, null);
            self.pipe(pid, llm_pid) catch {};
            return try std.fmt.allocPrint(self.allocator, "PID {d}", .{pid});
        } else if (std.mem.startsWith(u8, line, "EXEC ")) {
            const cmd = std.mem.trim(u8, line["EXEC ".len..], " \t");
            return try self.exec(cmd);
        } else if (std.mem.startsWith(u8, line, "KILL ")) {
            const pid_str = std.mem.trim(u8, line["KILL ".len..], " \t");
            const pid = std.fmt.parseInt(u32, pid_str, 10) catch return error.InvalidPid;
            try self.kill(pid);
            return try self.allocator.dupe(u8, "OK");
        } else if (std.mem.startsWith(u8, line, "SEND ")) {
            const rest = std.mem.trim(u8, line["SEND ".len..], " \t");
            const space_idx = std.mem.indexOfScalar(u8, rest, ' ') orelse return error.InvalidArgs;
            const pid_str = rest[0..space_idx];
            const data = rest[space_idx + 1 ..];
            const pid = std.fmt.parseInt(u32, pid_str, 10) catch return error.InvalidPid;
            try self.sendDecoded(pid, data);
            return null;
        } else {
            return null;
        }
    }

    /// Pipe: background thread copying stdout of A → stdin of B.
    fn pipe(self: *Kernel, pid_a: u32, pid_b: u32) !void {
        if (pid_a >= max_processes or pid_b >= max_processes) return error.InvalidPid;
        const proc_a = &(self.processes[pid_a] orelse return error.InvalidPid);
        const proc_b = &(self.processes[pid_b] orelse return error.InvalidPid);

        if (!proc_a.alive or !proc_b.alive) return error.ProcessDead;

        const src = proc_a.child.stdout orelse return error.NoStdout;
        const dst = proc_b.child.stdin orelse return error.NoStdin;

        _ = try std.Thread.spawn(.{}, pipeThread, .{ src, dst });
    }

    fn pipeThread(src: std.fs.File, dst: std.fs.File) void {
        var buf: [4096]u8 = undefined;
        while (true) {
            const n = src.read(&buf) catch break;
            if (n == 0) break;
            dst.writeAll(buf[0..n]) catch break;
        }
    }
};

// --- Tests ---

const testing = std.testing;

test "parseArgv: simple command" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const argv = try k.parseArgv("echo hello world");
    defer testing.allocator.free(argv);

    try testing.expectEqual(@as(usize, 3), argv.len);
    try testing.expectEqualStrings("echo", argv[0]);
    try testing.expectEqualStrings("hello", argv[1]);
    try testing.expectEqualStrings("world", argv[2]);
}

test "parseArgv: single quotes" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const argv = try k.parseArgv("sh -c 'echo hello world'");
    defer testing.allocator.free(argv);

    try testing.expectEqual(@as(usize, 3), argv.len);
    try testing.expectEqualStrings("sh", argv[0]);
    try testing.expectEqualStrings("-c", argv[1]);
    try testing.expectEqualStrings("echo hello world", argv[2]);
}

test "parseArgv: double quotes" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const argv = try k.parseArgv("sh -c \"echo hello\"");
    defer testing.allocator.free(argv);

    try testing.expectEqual(@as(usize, 3), argv.len);
    try testing.expectEqualStrings("echo hello", argv[2]);
}

test "parseArgv: extra whitespace" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const argv = try k.parseArgv("  echo   hello  ");
    defer testing.allocator.free(argv);

    try testing.expectEqual(@as(usize, 2), argv.len);
    try testing.expectEqualStrings("echo", argv[0]);
    try testing.expectEqualStrings("hello", argv[1]);
}

test "parseArgv: empty string" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const result = k.parseArgv("");
    try testing.expectError(error.EmptyCommand, result);
}

test "parseArgv: only whitespace" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const result = k.parseArgv("   ");
    try testing.expectError(error.EmptyCommand, result);
}

test "spawn and kill" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const pid = try k.spawn("sleep 10", null);
    try testing.expect(pid >= 1);
    try testing.expect(k.processes[pid] != null);
    try testing.expect(k.processes[pid].?.alive);

    try k.kill(pid);
    try testing.expect(k.processes[pid] == null);
}

test "kill: invalid pid" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    try testing.expectError(error.InvalidPid, k.kill(999));
    try testing.expectError(error.InvalidPid, k.kill(1));
}

test "pid reuse after kill" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const pid1 = try k.spawn("sleep 10", null);
    const pid2 = try k.spawn("sleep 10", null);

    try k.kill(pid1);

    const pid3 = try k.spawn("sleep 10", null);
    try testing.expectEqual(pid1, pid3);

    try k.kill(pid2);
    try k.kill(pid3);
}

test "exec: stdout" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const result = try k.exec("echo hello");
    defer testing.allocator.free(result);

    try testing.expectEqualStrings("hello", result);
}

test "exec: empty output returns OK" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const result = try k.exec("true");
    defer testing.allocator.free(result);

    try testing.expectEqualStrings("OK", result);
}

test "exec: stderr on failure" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const result = try k.exec("cat /nonexistent_file_12345");
    defer testing.allocator.free(result);

    try testing.expect(std.mem.startsWith(u8, result, "ERROR:"));
}

test "exec: empty lines replaced with space" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const result = try k.exec("sh -c 'printf \"a\\n\\nb\"'");
    defer testing.allocator.free(result);

    // Empty line between a and b should become space
    try testing.expect(std.mem.indexOf(u8, result, "a\n \nb") != null);
}

test "spawn and send" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    // cat echoes stdin to stdout
    const pid = try k.spawn("cat", null);

    try k.send(pid, "hello");

    // Read back from stdout
    const stdout = k.processes[pid].?.child.stdout.?;
    var buf: [64]u8 = undefined;
    const n = try stdout.read(&buf);

    try testing.expectEqualStrings("hello\n", buf[0..n]);

    try k.kill(pid);
}

test "sendDecoded: newline escape" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const pid = try k.spawn("cat", null);

    try k.sendDecoded(pid, "line1\\nline2");

    const stdout = k.processes[pid].?.child.stdout.?;
    var buf: [64]u8 = undefined;
    const n = try stdout.read(&buf);

    try testing.expectEqualStrings("line1\nline2\n", buf[0..n]);

    try k.kill(pid);
}

test "sendDecoded: no escapes" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const pid = try k.spawn("cat", null);

    try k.sendDecoded(pid, "plain text");

    const stdout = k.processes[pid].?.child.stdout.?;
    var buf: [64]u8 = undefined;
    const n = try stdout.read(&buf);

    try testing.expectEqualStrings("plain text\n", buf[0..n]);

    try k.kill(pid);
}

test "executeCommand: SPAWN" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    // Need a dummy LLM process for auto-pipe (SPAWN pipes to llm_pid)
    const llm_pid = try k.spawn("cat", null);

    const result = try k.executeCommand("SPAWN sleep 10", llm_pid);
    try testing.expect(result != null);
    try testing.expect(std.mem.startsWith(u8, result.?, "PID "));
    testing.allocator.free(result.?);
}

test "executeCommand: EXEC" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const llm_pid = try k.spawn("cat", null);

    const result = try k.executeCommand("EXEC echo test123", llm_pid);
    try testing.expect(result != null);
    try testing.expectEqualStrings("test123", result.?);
    testing.allocator.free(result.?);
}

test "executeCommand: KILL" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const llm_pid = try k.spawn("cat", null);
    const pid = try k.spawn("sleep 10", null);

    const cmd = try std.fmt.allocPrint(testing.allocator, "KILL {d}", .{pid});
    defer testing.allocator.free(cmd);

    const result = try k.executeCommand(cmd, llm_pid);
    try testing.expect(result != null);
    try testing.expectEqualStrings("OK", result.?);
    testing.allocator.free(result.?);

    try testing.expect(k.processes[pid] == null);
}

test "executeCommand: SEND returns null" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const llm_pid = try k.spawn("cat", null);
    const target = try k.spawn("cat", null);

    const cmd = try std.fmt.allocPrint(testing.allocator, "SEND {d} hello", .{target});
    defer testing.allocator.free(cmd);

    const result = try k.executeCommand(cmd, llm_pid);
    try testing.expect(result == null);
}

test "executeCommand: unknown command returns null" {
    var k = Kernel.init(testing.allocator);
    defer k.deinit();

    const llm_pid = try k.spawn("cat", null);

    const result = try k.executeCommand("FOOBAR something", llm_pid);
    try testing.expect(result == null);
}
