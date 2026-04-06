const std = @import("std");
const posix = std.posix;
const json = std.json;

// --- Constants ---

const max_clients = 64;
const max_spawned = 256;
const max_msg_types = 16;
const max_sessions = 16;
const max_poll_fds = max_clients + 1; // +1 for server fd
const read_buf_size = 4096;
const max_msg_size = 1024 * 1024;

// --- Message type IDs ---
// Fixed enum for fast matching, no heap strings.

const MsgType = enum(u8) {
    connect,
    connected,
    join,
    joined,
    message,
    stream_start,
    stream_delta,
    stream_end,
    tool_use,
    tool_result,
    done,
    cancel,
    init,
    @"error",
    unknown,

    fn fromString(s: []const u8) MsgType {
        const map = std.StaticStringMap(MsgType).initComptime(.{
            .{ "connect", .connect },
            .{ "connected", .connected },
            .{ "join", .join },
            .{ "joined", .joined },
            .{ "message", .message },
            .{ "stream_start", .stream_start },
            .{ "stream_delta", .stream_delta },
            .{ "stream_end", .stream_end },
            .{ "tool_use", .tool_use },
            .{ "tool_result", .tool_result },
            .{ "done", .done },
            .{ "cancel", .cancel },
            .{ "init", .init },
            .{ "error", .@"error" },
        });
        return map.get(s) orelse .unknown;
    }

    fn toString(self: MsgType) []const u8 {
        return switch (self) {
            .connect => "connect",
            .connected => "connected",
            .join => "join",
            .joined => "joined",
            .message => "message",
            .stream_start => "stream_start",
            .stream_delta => "stream_delta",
            .stream_end => "stream_end",
            .tool_use => "tool_use",
            .tool_result => "tool_result",
            .done => "done",
            .cancel => "cancel",
            .init => "init",
            .@"error" => "error",
            .unknown => "unknown",
        };
    }
};

// --- Bitset for message type subscriptions ---

const MsgTypeSet = packed struct {
    bits: u16 = 0,

    fn add(self: *MsgTypeSet, t: MsgType) void {
        const idx: u4 = @truncate(@intFromEnum(t));
        self.bits |= @as(u16, 1) << idx;
    }

    fn contains(self: MsgTypeSet, t: MsgType) bool {
        const idx: u4 = @truncate(@intFromEnum(t));
        return (self.bits & (@as(u16, 1) << idx)) != 0;
    }
};

// --- Client ---

const Client = struct {
    fd: posix.fd_t,
    name: [64]u8,
    name_len: u8,
    sends: MsgTypeSet,
    receives: MsgTypeSet,
    session: [64]u8,
    session_len: u8,
    read_buf: [read_buf_size]u8,
    read_pos: usize,
    connected: bool, // received "connect" handshake
    id: u32,

    fn getName(self: *const Client) []const u8 {
        return self.name[0..self.name_len];
    }

    fn getSession(self: *const Client) []const u8 {
        return self.session[0..self.session_len];
    }
};

// --- Spawned process ---

const SpawnedProcess = struct {
    child: std.process.Child,
    argv: []const []const u8,
    alive: bool,
};

// --- Kernel ---

pub const Kernel = struct {
    allocator: std.mem.Allocator,
    server_fd: posix.fd_t,
    socket_path: []const u8,
    system_prompt: []const u8,
    tools_json: []const u8,

    clients: [max_clients]?Client,
    next_client_id: u32,

    spawned: [max_spawned]?SpawnedProcess,

    running: bool,

    pub fn init(allocator: std.mem.Allocator, socket_path: []const u8, system_prompt: []const u8, tools_json: []const u8) !*Kernel {
        // Clean up stale socket
        std.fs.cwd().deleteFile(socket_path) catch {};

        // Create Unix socket
        const fd = try posix.socket(posix.AF.UNIX, posix.SOCK.STREAM, 0);
        errdefer posix.close(fd);

        // Bind
        var addr: posix.sockaddr.un = .{ .path = undefined, .family = posix.AF.UNIX };
        if (socket_path.len > addr.path.len) return error.PathTooLong;
        @memcpy(addr.path[0..socket_path.len], socket_path);
        if (socket_path.len < addr.path.len) {
            addr.path[socket_path.len] = 0;
        }

        try posix.bind(fd, @ptrCast(&addr), @sizeOf(posix.sockaddr.un));
        try posix.listen(fd, 16);

        log("listening on {s}", .{socket_path});

        const self = try allocator.create(Kernel);
        self.* = Kernel{
            .allocator = allocator,
            .server_fd = fd,
            .socket_path = socket_path,
            .system_prompt = system_prompt,
            .tools_json = tools_json,
            .clients = [_]?Client{null} ** max_clients,
            .next_client_id = 1,
            .spawned = [_]?SpawnedProcess{null} ** max_spawned,
            .running = true,
        };
        return self;
    }

    pub fn deinit(self: *Kernel) void {
        // Close all client fds
        for (&self.clients) |*slot| {
            if (slot.*) |*c| {
                posix.close(c.fd);
                slot.* = null;
            }
        }
        posix.close(self.server_fd);
        std.fs.cwd().deleteFile(self.socket_path) catch {};
        self.killAllSpawned();
        const allocator = self.allocator;
        allocator.destroy(self);
    }

    // --- Spawned processes (SPAWN/EXEC/KILL tools) ---

    fn findFreeSpawnSlot(self: *Kernel) !u32 {
        for (1..max_spawned) |i| {
            if (self.spawned[i] == null) return @intCast(i);
        }
        return error.TooManyProcesses;
    }

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
                try argv_list.append(try self.allocator.dupe(u8, command[start..i]));
                if (i < command.len) i += 1;
            } else {
                const start = i;
                while (i < command.len and command[i] != ' ') : (i += 1) {}
                try argv_list.append(try self.allocator.dupe(u8, command[start..i]));
            }
        }
        if (argv_list.items.len == 0) return error.EmptyCommand;

        return try self.allocator.dupe([]const u8, argv_list.items);
    }

    pub fn spawnProcess(self: *Kernel, command: []const u8, extra_env: ?*const std.process.EnvMap) !u32 {
        const pid = try self.findFreeSpawnSlot();
        const argv = try self.parseArgv(command);

        var child = std.process.Child.init(argv, self.allocator);
        child.stdin_behavior = .Inherit;
        child.stdout_behavior = .Inherit;
        // Suppress spawned process stderr to keep terminal clean
        child.stderr_behavior = .Ignore;

        if (extra_env) |e| {
            child.env_map = @constCast(e);
        }

        try child.spawn();

        self.spawned[pid] = SpawnedProcess{
            .child = child,
            .argv = argv,
            .alive = true,
        };

        log("spawned PID {d}: {s}", .{ pid, command });
        return pid;
    }

    fn killSpawned(self: *Kernel, pid: u32) !void {
        if (pid >= max_spawned) return error.InvalidPid;
        if (self.spawned[pid]) |*proc| {
            if (proc.alive) {
                _ = proc.child.kill() catch {};
                _ = proc.child.wait() catch {};
            }
            for (proc.argv) |arg| self.allocator.free(arg);
            self.allocator.free(proc.argv);
            self.spawned[pid] = null;
        } else {
            return error.InvalidPid;
        }
    }

    fn killAllSpawned(self: *Kernel) void {
        for (1..max_spawned) |i| {
            if (self.spawned[i]) |*proc| {
                if (proc.alive) {
                    _ = proc.child.kill() catch {};
                    _ = proc.child.wait() catch {};
                }
                for (proc.argv) |arg| self.allocator.free(arg);
                self.allocator.free(proc.argv);
                self.spawned[i] = null;
            }
        }
    }

    fn execSync(self: *Kernel, command: []const u8) ![]const u8 {
        const argv = try self.parseArgv(command);
        defer {
            for (argv) |arg| self.allocator.free(arg);
            self.allocator.free(argv);
        }

        var child = std.process.Child.init(argv, self.allocator);
        child.stdin_behavior = .Close;
        child.stdout_behavior = .Pipe;
        child.stderr_behavior = .Pipe;

        try child.spawn();

        var output = std.ArrayList(u8).init(self.allocator);
        defer output.deinit();

        const stdout = child.stdout orelse return error.NoStdout;
        var buf: [4096]u8 = undefined;
        while (true) {
            const n = stdout.read(&buf) catch break;
            if (n == 0) break;
            try output.appendSlice(buf[0..n]);
        }

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

        if (output.items.len == 0 and err_output.items.len > 0) {
            try output.appendSlice("ERROR: ");
            try output.appendSlice(err_output.items);
        }

        const result = std.mem.trim(u8, output.items, " \t\r\n");
        if (result.len == 0) return try self.allocator.dupe(u8, "OK");
        return try self.allocator.dupe(u8, result);
    }

    fn listProcesses(self: *Kernel) ![]const u8 {
        var result = std.ArrayList(u8).init(self.allocator);
        var found = false;
        for (1..max_spawned) |i| {
            if (self.spawned[i]) |proc| {
                if (found) try result.appendSlice(", ");
                found = true;
                const name = if (proc.argv.len > 0) proc.argv[0] else "(unknown)";
                try result.writer().print("PID {d} {s} alive={}", .{ i, name, proc.alive });
            }
        }
        if (!found) try result.appendSlice("(empty)");
        return result.toOwnedSlice();
    }

    fn reapZombies(self: *Kernel) void {
        for (1..max_spawned) |i| {
            if (self.spawned[i]) |*proc| {
                if (!proc.alive) continue;
                const result = posix.waitpid(proc.child.id, posix.W.NOHANG);
                if (result.pid != 0) {
                    proc.alive = false;
                    log("process {d} died", .{i});

                    // Notify all sessions about process death
                    const err_msg = std.fmt.allocPrint(self.allocator, "{{\"type\":\"error\",\"text\":\"process {d} died\"}}", .{i}) catch continue;
                    defer self.allocator.free(err_msg);
                    // Broadcast to all connected clients that receive "error"
                    for (0..max_clients) |ci| {
                        if (self.clients[ci]) |*c| {
                            if (c.connected and c.receives.contains(.@"error")) {
                                self.sendToClient(ci, err_msg);
                            }
                        }
                    }

                    // Clean up: free argv and clear slot for reuse
                    for (proc.argv) |arg| self.allocator.free(arg);
                    self.allocator.free(proc.argv);
                    self.spawned[i] = null;
                }
            }
        }
    }

    // --- Client management ---

    fn findClientSlot(self: *Kernel) ?usize {
        for (0..max_clients) |i| {
            if (self.clients[i] == null) return i;
        }
        return null;
    }

    fn acceptClient(self: *Kernel) !void {
        const client_fd = posix.accept(self.server_fd, null, null, 0) catch |err| {
            return err;
        };

        const slot = self.findClientSlot() orelse {
            posix.close(client_fd);
            log("too many clients, rejecting", .{});
            return;
        };

        self.clients[slot] = Client{
            .fd = client_fd,
            .name = undefined,
            .name_len = 0,
            .sends = .{},
            .receives = .{},
            .session = undefined,
            .session_len = 0,
            .read_buf = undefined,
            .read_pos = 0,
            .connected = false,
            .id = self.next_client_id,
        };
        self.next_client_id += 1;

        log("accepted connection, slot {d}, id {d}", .{ slot, self.next_client_id - 1 });
    }

    fn removeClient(self: *Kernel, slot: usize) void {
        if (self.clients[slot]) |*c| {
            log("client disconnected: {s} (id {d})", .{ c.getName(), c.id });
            posix.close(c.fd);
            self.clients[slot] = null;
        }
    }

    fn sendToClient(self: *Kernel, slot: usize, data: []const u8) void {
        if (data.len == 0) return;
        const c = &(self.clients[slot] orelse return);
        // Write data + newline
        _ = posix.write(c.fd, data) catch {
            return;
        };
        _ = posix.write(c.fd, "\n") catch {
            return;
        };
    }

    fn sendJsonToClient(self: *Kernel, slot: usize, msg: []const u8) void {
        self.sendToClient(slot, msg);
    }

    // --- Message routing ---

    fn broadcastToSession(self: *Kernel, session: []const u8, msg_type: MsgType, data: []const u8, exclude_slot: ?usize) void {
        for (0..max_clients) |i| {
            if (exclude_slot != null and i == exclude_slot.?) continue;
            if (self.clients[i]) |*c| {
                if (!c.connected) continue;
                if (c.session_len == 0) continue;
                if (!std.mem.eql(u8, c.getSession(), session)) continue;
                if (!c.receives.contains(msg_type)) continue;
                self.sendToClient(i, data);
            }
        }
    }

    // --- Tool execution ---

    fn handleToolUse(self: *Kernel, sender_session: []const u8, raw_json: []const u8) void {
        // Parse tool_use: {"type":"tool_use","id":"...","name":"...","input":{...}}
        const parsed = json.parseFromSlice(json.Value, self.allocator, raw_json, .{}) catch {
            log("failed to parse tool_use JSON", .{});
            return;
        };
        defer parsed.deinit();

        const obj = parsed.value.object;
        const id_val = obj.get("id") orelse return;
        const name_val = obj.get("name") orelse return;
        const input_val = obj.get("input") orelse return;

        const id_str = switch (id_val) {
            .string => |s| s,
            else => return,
        };
        const tool_name = switch (name_val) {
            .string => |s| s,
            else => return,
        };

        // Execute tool
        const result = self.executeTool(tool_name, input_val) catch |err| {
            const err_result = std.fmt.allocPrint(self.allocator, "{{\"type\":\"tool_result\",\"id\":\"{s}\",\"output\":\"ERROR: {}\"}}", .{ id_str, err }) catch return;
            defer self.allocator.free(err_result);
            self.broadcastToSession(sender_session, .tool_result, err_result, null);
            return;
        };
        defer self.allocator.free(result);

        // Escape result for JSON
        const escaped = self.jsonEscape(result) catch return;
        defer self.allocator.free(escaped);

        const response = std.fmt.allocPrint(self.allocator, "{{\"type\":\"tool_result\",\"id\":\"{s}\",\"output\":\"{s}\"}}", .{ id_str, escaped }) catch return;
        defer self.allocator.free(response);

        self.broadcastToSession(sender_session, .tool_result, response, null);
    }

    fn jsonEscape(self: *Kernel, input: []const u8) ![]const u8 {
        var out = std.ArrayList(u8).init(self.allocator);
        for (input) |c| {
            switch (c) {
                '"' => try out.appendSlice("\\\""),
                '\\' => try out.appendSlice("\\\\"),
                '\n' => try out.appendSlice("\\n"),
                '\r' => try out.appendSlice("\\r"),
                '\t' => try out.appendSlice("\\t"),
                else => {
                    if (c < 0x20) {
                        try out.writer().print("\\u{x:0>4}", .{c});
                    } else {
                        try out.append(c);
                    }
                },
            }
        }
        return out.toOwnedSlice();
    }

    fn executeTool(self: *Kernel, name: []const u8, input_val: json.Value) ![]const u8 {
        const obj = switch (input_val) {
            .object => |o| o,
            else => return error.InvalidArgs,
        };

        if (std.mem.eql(u8, name, "EXEC")) {
            const cmd = switch (obj.get("command") orelse return error.InvalidArgs) {
                .string => |s| s,
                else => return error.InvalidArgs,
            };
            return self.execSync(cmd);
        } else if (std.mem.eql(u8, name, "SPAWN")) {
            const cmd = switch (obj.get("command") orelse return error.InvalidArgs) {
                .string => |s| s,
                else => return error.InvalidArgs,
            };
            const pid = try self.spawnProcess(cmd, null);
            return std.fmt.allocPrint(self.allocator, "PID {d}", .{pid});
        } else if (std.mem.eql(u8, name, "KILL")) {
            const pid_val = obj.get("pid") orelse return error.InvalidArgs;
            const pid: u32 = switch (pid_val) {
                .integer => |i| @intCast(i),
                else => return error.InvalidArgs,
            };
            try self.killSpawned(pid);
            return try self.allocator.dupe(u8, "OK");
        } else if (std.mem.eql(u8, name, "LIST")) {
            return self.listProcesses();
        } else {
            return error.UnknownTool;
        }
    }

    // --- Cancel handling ---

    fn handleCancel(self: *Kernel, sender_session: []const u8) void {
        // Find all spawned processes and send SIGINT
        // For now: find client in same session that sends "stream_delta" (likely the driver)
        // and kill its associated process.
        // Simple approach: SIGINT all spawned processes
        _ = sender_session;
        for (1..max_spawned) |i| {
            if (self.spawned[i]) |*proc| {
                if (proc.alive) {
                    _ = posix.kill(proc.child.id, posix.SIG.INT) catch {};
                }
            }
        }
    }

    // --- Protocol handling ---

    fn handleConnect(self: *Kernel, slot: usize, raw_json: []const u8) void {
        const parsed = json.parseFromSlice(json.Value, self.allocator, raw_json, .{}) catch return;
        defer parsed.deinit();

        const obj = parsed.value.object;
        const c = &(self.clients[slot] orelse return);

        // Name
        if (obj.get("name")) |name_val| {
            const name_str = switch (name_val) {
                .string => |s| s,
                else => "",
            };
            const len = @min(name_str.len, 64);
            @memcpy(c.name[0..len], name_str[0..len]);
            c.name_len = @intCast(len);
        }

        // Sends
        if (obj.get("sends")) |sends_val| {
            switch (sends_val) {
                .array => |arr| {
                    for (arr.items) |item| {
                        switch (item) {
                            .string => |s| c.sends.add(MsgType.fromString(s)),
                            else => {},
                        }
                    }
                },
                else => {},
            }
        }

        // Receives
        if (obj.get("receives")) |receives_val| {
            switch (receives_val) {
                .array => |arr| {
                    for (arr.items) |item| {
                        switch (item) {
                            .string => |s| c.receives.add(MsgType.fromString(s)),
                            else => {},
                        }
                    }
                },
                else => {},
            }
        }

        c.connected = true;

        const response = std.fmt.allocPrint(self.allocator, "{{\"type\":\"connected\",\"id\":\"c{d}\"}}", .{c.id}) catch return;
        defer self.allocator.free(response);
        self.sendToClient(slot, response);

        log("client connected: {s} (id c{d})", .{ c.getName(), c.id });
    }

    fn handleJoin(self: *Kernel, slot: usize, raw_json: []const u8) void {
        const parsed = json.parseFromSlice(json.Value, self.allocator, raw_json, .{}) catch return;
        defer parsed.deinit();

        const obj = parsed.value.object;
        const c = &(self.clients[slot] orelse return);

        if (obj.get("session")) |session_val| {
            const session_str = switch (session_val) {
                .string => |s| s,
                else => return,
            };
            const len = @min(session_str.len, 64);
            @memcpy(c.session[0..len], session_str[0..len]);
            c.session_len = @intCast(len);
        }

        const response = std.fmt.allocPrint(self.allocator, "{{\"type\":\"joined\",\"session\":\"{s}\"}}", .{c.getSession()}) catch return;
        defer self.allocator.free(response);
        self.sendToClient(slot, response);

        log("client {s} joined session {s}", .{ c.getName(), c.getSession() });

        // If this client receives "init", send it now
        if (c.receives.contains(.init)) {
            self.sendInit(slot);
        }
    }

    fn sendInit(self: *Kernel, slot: usize) void {
        var buf = std.ArrayList(u8).init(self.allocator);
        defer buf.deinit();

        buf.appendSlice("{\"type\":\"init\",\"prompt\":") catch return;
        self.writeJsonString(&buf, self.system_prompt) catch return;
        buf.appendSlice(",\"tools\":") catch return;
        buf.appendSlice(self.tools_json) catch return;
        buf.appendSlice("}") catch return;

        self.sendToClient(slot, buf.items);
        log("sent init to client (slot {d})", .{slot});
    }

    fn writeJsonString(self: *Kernel, buf: *std.ArrayList(u8), s: []const u8) !void {
        _ = self;
        try buf.append('"');
        for (s) |c| {
            switch (c) {
                '"' => try buf.appendSlice("\\\""),
                '\\' => try buf.appendSlice("\\\\"),
                '\n' => try buf.appendSlice("\\n"),
                '\r' => try buf.appendSlice("\\r"),
                '\t' => try buf.appendSlice("\\t"),
                else => {
                    if (c < 0x20) {
                        try buf.writer().print("\\u{x:0>4}", .{c});
                    } else {
                        try buf.append(c);
                    }
                },
            }
        }
        try buf.append('"');
    }

    fn handleMessage(self: *Kernel, slot: usize, raw_json: []const u8) void {
        // Parse type
        const parsed = json.parseFromSlice(json.Value, self.allocator, raw_json, .{}) catch return;
        defer parsed.deinit();

        const obj = parsed.value.object;
        const type_str = switch (obj.get("type") orelse return) {
            .string => |s| s,
            else => return,
        };

        const msg_type = MsgType.fromString(type_str);
        const c = &(self.clients[slot] orelse return);

        // Validate: client can only send types in their "sends" list
        if (!c.sends.contains(msg_type)) {
            log("client {s} not allowed to send {s}", .{ c.getName(), type_str });
            return;
        }

        const session = c.getSession();
        if (session.len == 0) {
            log("client {s} not in a session", .{c.getName()});
            return;
        }

        // Cross-session routing: if message has "session" field, deliver there
        const target_session = blk: {
            if (obj.get("session")) |session_val| {
                switch (session_val) {
                    .string => |s| {
                        if (s.len > 0) break :blk s;
                    },
                    else => {},
                }
            }
            break :blk session;
        };

        switch (msg_type) {
            .tool_use => self.handleToolUse(session, raw_json),
            .cancel => self.handleCancel(session),
            else => self.broadcastToSession(target_session, msg_type, raw_json, slot),
        }
    }

    fn processClientData(self: *Kernel, slot: usize) void {
        const c = &(self.clients[slot] orelse return);

        // Process complete lines in read_buf
        while (true) {
            const data = c.read_buf[0..c.read_pos];
            const newline_pos = std.mem.indexOfScalar(u8, data, '\n') orelse break;

            const line = std.mem.trim(u8, data[0..newline_pos], " \t\r");

            if (line.len > 0) {
                // Determine message type for routing
                const mt = self.extractMsgType(line) orelse continue;

                switch (mt) {
                    .connect => self.handleConnect(slot, line),
                    .join => self.handleJoin(slot, line),
                    else => {
                        if (c.connected) {
                            self.handleMessage(slot, line);
                        }
                    },
                }
            }

            // Shift remaining data
            const remaining = c.read_pos - newline_pos - 1;
            if (remaining > 0) {
                std.mem.copyForwards(u8, c.read_buf[0..remaining], data[newline_pos + 1 .. c.read_pos]);
            }
            c.read_pos = remaining;
        }
    }

    fn extractMsgType(self: *Kernel, line: []const u8) ?MsgType {
        _ = self;
        // Fast: parse just enough to get "type":"..."
        const parsed = json.parseFromSlice(json.Value, std.heap.page_allocator, line, .{}) catch return null;
        defer parsed.deinit();

        const obj = switch (parsed.value) {
            .object => |o| o,
            else => return null,
        };

        const type_val = obj.get("type") orelse return null;
        const type_str = switch (type_val) {
            .string => |s| s,
            else => return null,
        };

        return MsgType.fromString(type_str);
    }

    // --- Main loop ---

    pub fn run(self: *Kernel) !void {
        log("entering main loop", .{});

        while (self.running) {
            // Build poll fds
            var fds: [max_poll_fds]posix.pollfd = undefined;
            var fd_map: [max_poll_fds]?usize = undefined; // maps poll index → client slot
            var nfds: usize = 0;

            // Server fd
            fds[0] = .{
                .fd = self.server_fd,
                .events = posix.POLL.IN,
                .revents = 0,
            };
            fd_map[0] = null;
            nfds = 1;

            // Client fds
            for (0..max_clients) |i| {
                if (self.clients[i]) |*c| {
                    if (nfds >= max_poll_fds) break;
                    fds[nfds] = .{
                        .fd = c.fd,
                        .events = posix.POLL.IN,
                        .revents = 0,
                    };
                    fd_map[nfds] = i;
                    nfds += 1;
                }
            }

            // Poll with 100ms timeout (for zombie reaping)
            const ready = posix.poll(fds[0..nfds], 100) catch |err| {
                if (err == error.Interrupted) continue;
                return err;
            };

            if (ready == 0) {
                self.reapZombies();
                continue;
            }

            // Process events
            var i: usize = 0;
            while (i < nfds) : (i += 1) {
                if (fds[i].revents == 0) continue;

                if (i == 0) {
                    // Server: accept new connection
                    try self.acceptClient();
                    continue;
                }

                const client_slot = fd_map[i] orelse continue;

                if (fds[i].revents & posix.POLL.HUP != 0 or
                    fds[i].revents & posix.POLL.ERR != 0)
                {
                    self.removeClient(client_slot);
                    continue;
                }

                if (fds[i].revents & posix.POLL.IN != 0) {
                    const c = &(self.clients[client_slot] orelse continue);
                    const space = c.read_buf[c.read_pos..];
                    if (space.len == 0) {
                        // Buffer overflow, disconnect
                        self.removeClient(client_slot);
                        continue;
                    }

                    const n = posix.read(c.fd, space) catch {
                        self.removeClient(client_slot);
                        continue;
                    };

                    if (n == 0) {
                        self.removeClient(client_slot);
                        continue;
                    }

                    c.read_pos += n;
                    self.processClientData(client_slot);
                }
            }

            self.reapZombies();
        }
    }

    pub fn stop(self: *Kernel) void {
        self.running = false;
    }
};

// --- Logging ---

var verbose: bool = false;

pub fn setVerbose(v: bool) void {
    verbose = v;
}

fn log(comptime fmt: []const u8, args: anytype) void {
    if (verbose) {
        std.debug.print("[kernel] " ++ fmt ++ "\n", args);
    }
}

// --- Signal handling ---

var global_kernel: ?*Kernel = null;

fn handleSignal(_: c_int) callconv(.C) void {
    if (global_kernel) |k| {
        k.running = false;
    }
}

pub fn installSignalHandlers(k: *Kernel) void {
    global_kernel = k;
    const act = posix.Sigaction{
        .handler = .{ .handler = handleSignal },
        .mask = posix.empty_sigset,
        .flags = 0,
    };
    posix.sigaction(posix.SIG.INT, &act, null);
    posix.sigaction(posix.SIG.TERM, &act, null);
}

// --- Tests ---

const testing = std.testing;

/// Create a Kernel for testing without opening a real socket.
fn initTestKernel(allocator: std.mem.Allocator) !*Kernel {
    const self = try allocator.create(Kernel);
    self.* = Kernel{
        .allocator = allocator,
        .server_fd = -1,
        .socket_path = "",
        .system_prompt = "",
        .tools_json = "[]",
        .clients = [_]?Client{null} ** max_clients,
        .next_client_id = 1,
        .spawned = [_]?SpawnedProcess{null} ** max_spawned,
        .running = true,
    };
    return self;
}

fn deinitTestKernel(self: *Kernel) void {
    self.killAllSpawned();
    self.allocator.destroy(self);
}

test "MsgType.fromString" {
    try testing.expectEqual(MsgType.message, MsgType.fromString("message"));
    try testing.expectEqual(MsgType.tool_use, MsgType.fromString("tool_use"));
    try testing.expectEqual(MsgType.unknown, MsgType.fromString("foobar"));
}

test "MsgTypeSet" {
    var set = MsgTypeSet{};
    try testing.expect(!set.contains(.message));
    set.add(.message);
    try testing.expect(set.contains(.message));
    try testing.expect(!set.contains(.cancel));
    set.add(.cancel);
    try testing.expect(set.contains(.cancel));
}

test "parseArgv: simple" {
    const k = try initTestKernel(testing.allocator);
    defer deinitTestKernel(k);

    const argv = try k.parseArgv("echo hello world");
    defer testing.allocator.free(argv);

    try testing.expectEqual(@as(usize, 3), argv.len);
    try testing.expectEqualStrings("echo", argv[0]);
    try testing.expectEqualStrings("hello", argv[1]);
    try testing.expectEqualStrings("world", argv[2]);
}

test "parseArgv: quotes" {
    const k = try initTestKernel(testing.allocator);
    defer deinitTestKernel(k);

    const argv = try k.parseArgv("sh -c 'echo hello world'");
    defer testing.allocator.free(argv);

    try testing.expectEqual(@as(usize, 3), argv.len);
    try testing.expectEqualStrings("echo hello world", argv[2]);
}

test "parseArgv: empty" {
    const k = try initTestKernel(testing.allocator);
    defer deinitTestKernel(k);

    const result = k.parseArgv("");
    try testing.expectError(error.EmptyCommand, result);
}

test "execSync: echo" {
    const k = try initTestKernel(testing.allocator);
    defer deinitTestKernel(k);

    const result = try k.execSync("echo hello");
    defer testing.allocator.free(result);

    try testing.expectEqualStrings("hello", result);
}

test "execSync: empty output" {
    const k = try initTestKernel(testing.allocator);
    defer deinitTestKernel(k);

    const result = try k.execSync("true");
    defer testing.allocator.free(result);

    try testing.expectEqualStrings("OK", result);
}

test "jsonEscape" {
    const k = try initTestKernel(testing.allocator);
    defer deinitTestKernel(k);

    const result = try k.jsonEscape("hello\nworld\"test\\");
    defer testing.allocator.free(result);

    try testing.expectEqualStrings("hello\\nworld\\\"test\\\\", result);
}
