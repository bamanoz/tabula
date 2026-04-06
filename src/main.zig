const std = @import("std");
const kernel = @import("kernel.zig");

// --- Minimal YAML config (just boot command) ---

fn readBootCmd(allocator: std.mem.Allocator, path: []const u8) ![]const u8 {
    const raw = std.fs.cwd().readFileAlloc(allocator, path, 1024 * 64) catch |err| {
        std.debug.print("error: cannot read {s}: {}\n", .{ path, err });
        std.process.exit(1);
    };
    defer allocator.free(raw);

    var line_iter = std.mem.splitScalar(u8, raw, '\n');
    while (line_iter.next()) |line| {
        const trimmed = std.mem.trim(u8, line, " \t\r");
        if (trimmed.len == 0 or trimmed[0] == '#') continue;

        if (std.mem.startsWith(u8, trimmed, "boot:")) {
            const val = std.mem.trim(u8, trimmed["boot:".len..], " \t");
            if (val.len > 0) return try allocator.dupe(u8, val);
        }
    }

    std.debug.print("error: tabula.yaml must specify 'boot'\n", .{});
    std.process.exit(1);
}

// --- Boot script execution → JSON config ---

const BootConfig = struct {
    socket: []const u8,
    system_prompt: []const u8,
    spawn: []const []const u8,
};

fn runBoot(allocator: std.mem.Allocator, cmd: []const u8) !BootConfig {
    var child = std.process.Child.init(
        &.{ "sh", "-c", cmd },
        allocator,
    );
    child.stdout_behavior = .Pipe;
    child.stderr_behavior = .Inherit;
    try child.spawn();

    const output = try child.stdout.?.reader().readAllAlloc(allocator, 1024 * 1024);

    const term = child.wait() catch |err| {
        std.debug.print("error: boot script failed: {}\n", .{err});
        std.process.exit(1);
    };
    if (term.Exited != 0) {
        std.debug.print("error: boot script exited with code {d}\n", .{term.Exited});
        std.process.exit(1);
    }

    // Parse JSON
    const parsed = std.json.parseFromSlice(struct {
        socket: []const u8,
        system_prompt: []const u8,
        spawn: []const []const u8 = &.{},
    }, allocator, output, .{}) catch |err| {
        std.debug.print("error: cannot parse boot output: {}\n", .{err});
        std.process.exit(1);
    };
    defer parsed.deinit();

    // Dupe strings so they outlive parsed (must happen before freeing output)
    const socket = try allocator.dupe(u8, parsed.value.socket);
    const prompt = try allocator.dupe(u8, parsed.value.system_prompt);
    var spawn_list = std.ArrayList([]const u8).init(allocator);
    for (parsed.value.spawn) |s| {
        try spawn_list.append(try allocator.dupe(u8, s));
    }

    allocator.free(output);

    return BootConfig{
        .socket = socket,
        .system_prompt = prompt,
        .spawn = try spawn_list.toOwnedSlice(),
    };
}

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    // 1. Resolve TABULA_HOME
    const tabula_home = std.process.getEnvVarOwned(allocator, "TABULA_HOME") catch blk: {
        const home = std.process.getEnvVarOwned(allocator, "HOME") catch {
            std.debug.print("error: neither TABULA_HOME nor HOME is set\n", .{});
            std.process.exit(1);
        };
        defer allocator.free(home);
        break :blk try std.fs.path.join(allocator, &.{ home, ".tabula" });
    };
    defer allocator.free(tabula_home);

    // 2. Parse CLI flags
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);
    var verbose = false;
    for (args[1..]) |arg| {
        if (std.mem.eql(u8, arg, "-v") or std.mem.eql(u8, arg, "--verbose")) verbose = true;
    }
    kernel.setVerbose(verbose);

    // 3. chdir to TABULA_HOME
    std.posix.chdir(tabula_home) catch |err| {
        std.debug.print("error: cannot chdir to {s}: {}\n", .{ tabula_home, err });
        std.process.exit(1);
    };
    if (verbose) std.debug.print("[main] working directory: {s}\n", .{tabula_home});

    // 4. Read minimal YAML config (just boot command)
    const config_path = try std.fs.path.join(allocator, &.{ tabula_home, "tabula.yaml" });
    defer allocator.free(config_path);

    const boot_cmd = try readBootCmd(allocator, config_path);
    defer allocator.free(boot_cmd);

    if (verbose) std.debug.print("[main] running boot: {s}\n", .{boot_cmd});

    // 5. Run boot script → get full config
    const boot = try runBoot(allocator, boot_cmd);
    defer allocator.free(boot.socket);
    defer allocator.free(boot.system_prompt);
    defer {
        for (boot.spawn) |s| allocator.free(s);
        allocator.free(boot.spawn);
    }

    if (verbose) std.debug.print("[main] socket: {s}, prompt: {d} bytes, spawn: {d} processes\n", .{ boot.socket, boot.system_prompt.len, boot.spawn.len });

    // 6. Load tools
    const tools_raw = @embedFile("kernel.tools.json");
    const tools_json = blk: {
        var compact = std.ArrayList(u8).init(allocator);
        var in_string = false;
        var prev_was_backslash = false;
        for (tools_raw) |c| {
            if (in_string) {
                try compact.append(c);
                if (c == '"' and !prev_was_backslash) in_string = false;
                prev_was_backslash = (c == '\\' and !prev_was_backslash);
            } else {
                if (c == '\n' or c == '\r' or c == ' ' or c == '\t') continue;
                try compact.append(c);
                if (c == '"') in_string = true;
            }
        }
        break :blk try compact.toOwnedSlice();
    };
    defer allocator.free(tools_json);

    // 7. Init kernel
    if (verbose) std.debug.print("[main] initializing kernel...\n", .{});
    const k = kernel.Kernel.init(allocator, boot.socket, boot.system_prompt, tools_json) catch |err| {
        std.debug.print("error: kernel init failed: {}\n", .{err});
        std.process.exit(1);
    };
    defer k.deinit();
    kernel.installSignalHandlers(k);

    // 8. Spawn processes from boot config
    for (boot.spawn) |cmd| {
        var env = std.process.EnvMap.init(allocator);
        defer env.deinit();

        var env_iter = std.process.getEnvMap(allocator) catch continue;
        defer env_iter.deinit();
        var it = env_iter.iterator();
        while (it.next()) |entry| {
            try env.put(entry.key_ptr.*, entry.value_ptr.*);
        }
        try env.put("TABULA_SOCKET", boot.socket);
        try env.put("TABULA_HOME", tabula_home);

        _ = k.spawnProcess(cmd, &env) catch |err| {
            if (verbose) std.debug.print("error: cannot spawn '{s}': {}\n", .{ cmd, err });
        };
    }

    if (verbose) std.debug.print("[main] ready, entering main loop\n", .{});

    // 9. Main loop
    k.run() catch |err| {
        std.debug.print("error: kernel loop failed: {}\n", .{err});
        std.process.exit(1);
    };
}
