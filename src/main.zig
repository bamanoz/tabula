const std = @import("std");
const kernel = @import("kernel.zig");

const Config = struct {
    socket: []const u8,
    system_prompt_cmd: []const u8,
    spawn: []const []const u8,
};

fn readConfig(allocator: std.mem.Allocator, path: []const u8) !Config {
    const raw = std.fs.cwd().readFileAlloc(allocator, path, 1024 * 64) catch |err| {
        std.debug.print("error: cannot read {s}: {}\n", .{ path, err });
        std.process.exit(1);
    };
    defer allocator.free(raw);

    var socket: ?[]const u8 = null;
    var system_prompt_cmd: ?[]const u8 = null;
    var spawn_list = std.ArrayList([]const u8).init(allocator);
    defer spawn_list.deinit();

    var in_spawn = false;

    var line_iter = std.mem.splitScalar(u8, raw, '\n');
    while (line_iter.next()) |line| {
        const trimmed = std.mem.trim(u8, line, " \t\r");
        if (trimmed.len == 0 or trimmed[0] == '#') {
            if (in_spawn and trimmed.len == 0) in_spawn = false;
            continue;
        }

        if (in_spawn) {
            if (std.mem.startsWith(u8, trimmed, "- ")) {
                const val = std.mem.trim(u8, trimmed["- ".len..], " \t");
                if (val.len > 0) try spawn_list.append(try allocator.dupe(u8, val));
            } else {
                in_spawn = false;
                // fall through to parse this line as a key
            }
        }

        if (!in_spawn) {
            if (std.mem.startsWith(u8, trimmed, "socket:")) {
                const val = std.mem.trim(u8, trimmed["socket:".len..], " \t");
                if (val.len > 0) socket = try allocator.dupe(u8, val);
            } else if (std.mem.startsWith(u8, trimmed, "system_prompt:")) {
                const val = std.mem.trim(u8, trimmed["system_prompt:".len..], " \t");
                if (val.len > 0) system_prompt_cmd = try allocator.dupe(u8, val);
            } else if (std.mem.eql(u8, trimmed, "spawn:")) {
                in_spawn = true;
            }
        }
    }

    if (socket == null) {
        std.debug.print("error: tabula.yaml must specify 'socket'\n", .{});
        std.process.exit(1);
    }
    if (system_prompt_cmd == null) {
        std.debug.print("error: tabula.yaml must specify 'system_prompt'\n", .{});
        std.process.exit(1);
    }

    return Config{
        .socket = socket.?,
        .system_prompt_cmd = system_prompt_cmd.?,
        .spawn = try spawn_list.toOwnedSlice(),
    };
}

fn runSystemPrompt(allocator: std.mem.Allocator, cmd: []const u8) ![]const u8 {
    var child = std.process.Child.init(
        &.{ "sh", "-c", cmd },
        allocator,
    );
    child.stdout_behavior = .Pipe;
    child.stderr_behavior = .Inherit;
    try child.spawn();

    const output = try child.stdout.?.reader().readAllAlloc(allocator, 1024 * 1024);

    const term = child.wait() catch |err| {
        std.debug.print("error: system_prompt failed: {}\n", .{err});
        std.process.exit(1);
    };
    if (term.Exited != 0) {
        std.debug.print("error: system_prompt exited with code {d}\n", .{term.Exited});
        std.process.exit(1);
    }

    // Parse JSON: {"prompt": "..."}
    const parsed = std.json.parseFromSlice(struct {
        prompt: []const u8,
    }, allocator, output, .{}) catch |err| {
        std.debug.print("error: cannot parse system_prompt output: {}\n", .{err});
        std.process.exit(1);
    };
    defer parsed.deinit();
    allocator.free(output);

    return try allocator.dupe(u8, parsed.value.prompt);
}

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();

    // 1. Read config from TABULA_HOME (default: ~/.tabula)
    const tabula_home = std.process.getEnvVarOwned(allocator, "TABULA_HOME") catch blk: {
        const home = std.process.getEnvVarOwned(allocator, "HOME") catch {
            std.debug.print("error: neither TABULA_HOME nor HOME is set\n", .{});
            std.process.exit(1);
        };
        defer allocator.free(home);
        break :blk try std.fs.path.join(allocator, &.{ home, ".tabula" });
    };
    defer allocator.free(tabula_home);

    const config_path = try std.fs.path.join(allocator, &.{ tabula_home, "tabula.yaml" });
    defer allocator.free(config_path);

    const config = try readConfig(allocator, config_path);
    defer allocator.free(config.socket);
    defer allocator.free(config.system_prompt_cmd);
    defer {
        for (config.spawn) |s| allocator.free(s);
        allocator.free(config.spawn);
    }

    // Verbose logging only with -v flag
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);
    var verbose = false;
    for (args[1..]) |arg| {
        if (std.mem.eql(u8, arg, "-v") or std.mem.eql(u8, arg, "--verbose")) verbose = true;
    }
    kernel.setVerbose(verbose);

    // Change working directory to ~/.tabula so relative paths in config work
    std.posix.chdir(tabula_home) catch |err| {
        std.debug.print("error: cannot chdir to {s}: {}\n", .{ tabula_home, err });
        std.process.exit(1);
    };
    if (verbose) std.debug.print("[main] working directory: {s}\n", .{tabula_home});

    // 2. Run system_prompt script
    const system_prompt = try runSystemPrompt(allocator, config.system_prompt_cmd);
    defer allocator.free(system_prompt);
    if (verbose) std.debug.print("[main] system prompt: {d} bytes\n", .{system_prompt.len});

    // 3. Load tools — compact JSON (strip newlines for JSON lines protocol)
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

    // 4. Init kernel (opens socket)
    if (verbose) std.debug.print("[main] initializing kernel...\n", .{});
    const k = kernel.Kernel.init(allocator, config.socket, system_prompt, tools_json) catch |err| {
        std.debug.print("error: kernel init failed: {}\n", .{err});
        std.process.exit(1);
    };
    defer k.deinit();
    kernel.installSignalHandlers(k);

    // 5. Spawn processes from config
    for (config.spawn) |cmd| {
        var env = std.process.EnvMap.init(allocator);
        defer env.deinit();

        // Inherit current environment
        var env_iter = std.process.getEnvMap(allocator) catch continue;
        defer env_iter.deinit();
        var it = env_iter.iterator();
        while (it.next()) |entry| {
            try env.put(entry.key_ptr.*, entry.value_ptr.*);
        }
        try env.put("TABULA_SOCKET", config.socket);
        try env.put("TABULA_HOME", tabula_home);

        _ = k.spawnProcess(cmd, &env) catch |err| {
            if (verbose) std.debug.print("error: cannot spawn '{s}': {}\n", .{ cmd, err });
        };
    }

    if (verbose) std.debug.print("[main] ready, entering main loop\n", .{});

    // 6. Main loop
    k.run() catch |err| {
        std.debug.print("error: kernel loop failed: {}\n", .{err});
        std.process.exit(1);
    };
}
