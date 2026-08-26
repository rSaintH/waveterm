// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { globalStore } from "@/app/store/jotaiStore";
import { TabRpcClient } from "@/app/store/wshrpcutil";
import { fireAndForget } from "@/util/util";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { isPathAtOrUnder, normalizeTreePath } from "./directory-tree-utils";
import { type PreviewModel } from "./preview-model";

async function loadChildDirs(model: PreviewModel, path: string, showHidden: boolean): Promise<FileInfo[]> {
    const remotePath = await model.formatRemoteUri(path, globalStore.get);
    const stream = model.env.rpc.FileListStreamCommand(TabRpcClient, { path: remotePath }, null);
    const dirs: FileInfo[] = [];
    for await (const chunk of stream) {
        if (!chunk?.fileinfo) {
            continue;
        }
        for (const fi of chunk.fileinfo) {
            if (!fi.isdir) {
                continue;
            }
            if (!showHidden && fi.name?.startsWith(".")) {
                continue;
            }
            dirs.push(fi);
        }
    }
    dirs.sort((a, b) => (a.name ?? "").localeCompare(b.name ?? ""));
    return dirs;
}

type TreeNodeProps = {
    model: PreviewModel;
    path: string;
    name: string;
    depth: number;
    currentPath: string;
    expanded: Set<string>;
    childrenMap: { [path: string]: FileInfo[] };
    loadState: { [path: string]: "loading" | "error" };
    toggle: (path: string) => void;
};

function TreeNode({ model, path, name, depth, currentPath, expanded, childrenMap, loadState, toggle }: TreeNodeProps) {
    const key = normalizeTreePath(path);
    const isOpen = expanded.has(key);
    const isCurrent = key == normalizeTreePath(currentPath);
    const children = childrenMap[key];
    const nodeState = loadState[key];

    return (
        <div className="dir-tree-node">
            <div
                className={`flex items-center gap-1 px-1 py-0.5 cursor-pointer rounded hover:bg-hoverbg ${isCurrent ? "bg-accent/20 text-accent" : ""}`}
                style={{ paddingLeft: depth * 12 + 4 }}
                onClick={() => fireAndForget(() => model.goHistory(path))}
            >
                <span
                    className="w-3 shrink-0 text-secondary"
                    onClick={(e) => {
                        e.stopPropagation();
                        toggle(path);
                    }}
                >
                    {isOpen ? "▾" : "▸"}
                </span>
                <span className="truncate">{name}</span>
            </div>
            {isOpen && (
                <div className="dir-tree-children">
                    {children != null &&
                        children.map((c) => (
                            <TreeNode
                                key={c.path}
                                model={model}
                                path={c.path}
                                name={c.name}
                                depth={depth + 1}
                                currentPath={currentPath}
                                expanded={expanded}
                                childrenMap={childrenMap}
                                loadState={loadState}
                                toggle={toggle}
                            />
                        ))}
                    {children == null && nodeState == "error" && (
                        <div className="text-error text-xs" style={{ paddingLeft: (depth + 1) * 12 + 4 }}>
                            ⚠ failed to load
                        </div>
                    )}
                    {children == null && nodeState != "error" && (
                        <div className="text-secondary text-xs" style={{ paddingLeft: (depth + 1) * 12 + 4 }}>
                            …
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

type DirectoryTreeProps = {
    model: PreviewModel;
    // Root the tree is drawn from. Reconciled by the caller so it survives navigating into
    // subfolders instead of following every directory change.
    anchor: string;
    // Directory currently shown in the file list; highlighted and revealed in the tree.
    currentPath: string;
    showHidden: boolean;
    // Bumped by a manual refresh or the external-change poll.
    refreshVersion: number;
};

const DirectoryTree = memo(({ model, anchor, currentPath, showHidden, refreshVersion }: DirectoryTreeProps) => {
    const [expanded, setExpanded] = useState<Set<string>>(new Set());
    const [childrenMap, setChildrenMap] = useState<{ [path: string]: FileInfo[] }>({});
    const [loadState, setLoadState] = useState<{ [path: string]: "loading" | "error" }>({});
    const [reloadNonce, setReloadNonce] = useState(0);
    const prevShowHidden = useRef(showHidden);
    const prevRefreshVersion = useRef(refreshVersion);
    const anchorKey = normalizeTreePath(anchor);

    const loadInto = useCallback(
        async (path: string) => {
            const key = normalizeTreePath(path);
            setLoadState((prev) => ({ ...prev, [key]: "loading" }));
            try {
                const dirs = await loadChildDirs(model, path, showHidden);
                setChildrenMap((prev) => ({ ...prev, [key]: dirs }));
                setLoadState((prev) => {
                    const next = { ...prev };
                    delete next[key];
                    return next;
                });
            } catch (e) {
                console.error("directory-tree load error", path, e);
                setLoadState((prev) => ({ ...prev, [key]: "error" }));
            }
        },
        [model, showHidden]
    );

    // A refresh has to reach the tree too, or the sidebar and the file list end up showing
    // different directories. Re-fetch every open node rather than clearing the cache:
    // clearing would blank expanded subtrees that are not on the current path, since only
    // that chain gets reloaded by the reveal effect below.
    useEffect(() => {
        if (prevRefreshVersion.current === refreshVersion) {
            return;
        }
        prevRefreshVersion.current = refreshVersion;
        for (const key of expanded) {
            fireAndForget(() => loadInto(key));
        }
    }, [refreshVersion]);

    // Hidden-file filtering happens at fetch time, so cached children go stale when the eye
    // toggle flips. Drop the cache and bump reloadNonce so the reveal effect re-runs against
    // an empty cache; clearing alone would not retrigger it, since its deps are unchanged.
    useEffect(() => {
        if (prevShowHidden.current === showHidden) {
            return;
        }
        prevShowHidden.current = showHidden;
        setChildrenMap({});
        setExpanded(new Set());
        setLoadState({});
        setReloadNonce((n) => n + 1);
    }, [showHidden]);

    const toggle = useCallback(
        (path: string) => {
            const key = normalizeTreePath(path);
            setExpanded((prev) => {
                const next = new Set(prev);
                if (next.has(key)) {
                    next.delete(key);
                } else {
                    next.add(key);
                    if (childrenMap[key] == null) {
                        fireAndForget(() => loadInto(path));
                    }
                }
                return next;
            });
        },
        [childrenMap, loadInto]
    );

    // Load the anchor's children and auto-expand from the anchor down to the current folder.
    useEffect(() => {
        if (anchorKey == "" || currentPath == "") {
            return;
        }
        fireAndForget(async () => {
            const toExpand: string[] = [anchorKey];
            if (childrenMap[anchorKey] == null) {
                await loadInto(anchorKey);
            }
            if (isPathAtOrUnder(anchorKey, currentPath) && anchorKey != normalizeTreePath(currentPath)) {
                // anchorKey may be a bare root that already ends in "/" ("/" or a Windows
                // drive root like "C:/"), so derive the prefix from the key itself instead of
                // special-casing only "/".
                const anchorPrefix = anchorKey.endsWith("/") ? anchorKey : anchorKey + "/";
                const rest = normalizeTreePath(currentPath).slice(anchorPrefix.length);
                let acc = anchorKey.endsWith("/") ? anchorKey.slice(0, -1) : anchorKey;
                for (const seg of rest.split("/").filter((s) => s != "")) {
                    acc = acc + "/" + seg;
                    const accKey = normalizeTreePath(acc);
                    if (childrenMap[accKey] == null) {
                        await loadInto(acc);
                    }
                    toExpand.push(accKey);
                }
            }
            setExpanded((prev) => {
                const next = new Set(prev);
                for (const k of toExpand) {
                    next.add(k);
                }
                return next;
            });
        });
    }, [anchorKey, currentPath, reloadNonce]);

    if (anchorKey == "") {
        return <div className="dir-tree-panel p-2 text-secondary text-xs">No folder</div>;
    }

    const anchorName = anchorKey == "/" ? "/" : anchorKey.split("/").pop() || anchorKey;
    return (
        <div className="dir-tree-panel overflow-auto text-sm select-none">
            <TreeNode
                model={model}
                path={anchorKey}
                name={anchorName}
                depth={0}
                currentPath={currentPath}
                expanded={expanded}
                childrenMap={childrenMap}
                loadState={loadState}
                toggle={toggle}
            />
        </div>
    );
});

DirectoryTree.displayName = "DirectoryTree";

export { DirectoryTree };
