const std = @import("std");

pub const Config = struct {
    llm_skill: ?[]const u8,
    genesis: ?[]const u8,

    raw: []const u8, // full file contents, owned

    pub fn deinit(self: *const Config, allocator: std.mem.Allocator) void {
        allocator.free(self.raw);
    }
};

/// Minimal YAML-subset parser. Only handles:
///   llm: <command>
///   genesis: |
///     multi-line block
pub fn parse(allocator: std.mem.Allocator, path: []const u8) !Config {
    const raw = try std.fs.cwd().readFileAlloc(allocator, path, 1024 * 64);

    var config = Config{
        .llm_skill = null,
        .genesis = null,
        .raw = raw,
    };

    var genesis_start: ?usize = null;
    var genesis_indent: ?usize = null;

    var line_iter = std.mem.splitSequence(u8, raw, "\n");
    while (line_iter.next()) |line| {
        // Track genesis block
        if (genesis_start != null) {
            if (line.len == 0) continue;
            const indent = countLeadingSpaces(line);
            if (genesis_indent) |gi| {
                if (indent < gi) {
                    const offset = line_iter.index.? - line.len - 1;
                    config.genesis = raw[genesis_start.?..offset];
                    genesis_start = null;
                    genesis_indent = null;
                } else {
                    continue;
                }
            } else {
                if (indent > 0) {
                    genesis_indent = indent;
                    genesis_start = line_iter.index.? - line.len - 1;
                }
                continue;
            }
        }

        const trimmed = std.mem.trim(u8, line, " \t\r");
        if (trimmed.len == 0 or trimmed[0] == '#') continue;

        if (extractValue(trimmed, "llm:")) |v| {
            config.llm_skill = v;
            continue;
        }

        if (std.mem.startsWith(u8, trimmed, "genesis:")) {
            const after = std.mem.trim(u8, trimmed["genesis:".len..], " \t");
            if (std.mem.eql(u8, after, "|")) {
                genesis_start = line_iter.index orelse raw.len;
                genesis_indent = null;
            } else if (after.len > 0) {
                config.genesis = after;
            }
            continue;
        }
    }

    // Genesis block extends to EOF
    if (genesis_start) |gs| {
        config.genesis = raw[gs..];
    }

    return config;
}

fn countLeadingSpaces(line: []const u8) usize {
    var count: usize = 0;
    for (line) |c| {
        if (c == ' ') {
            count += 1;
        } else if (c == '\t') {
            count += 2;
        } else break;
    }
    return count;
}

fn extractValue(line: []const u8, key: []const u8) ?[]const u8 {
    if (std.mem.startsWith(u8, line, key)) {
        const val = std.mem.trim(u8, line[key.len..], " \t");
        if (val.len > 0) return val;
    }
    return null;
}
