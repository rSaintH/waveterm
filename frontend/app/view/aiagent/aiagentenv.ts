// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type { WaveEnv, WaveEnvSubset } from "@/app/waveenv/waveenv";

export type AiAgentEnv = WaveEnvSubset<{
    rpc: {
        AiAgentListCommand: WaveEnv["rpc"]["AiAgentListCommand"];
        AiAgentRunCommand: WaveEnv["rpc"]["AiAgentRunCommand"];
        AiAgentSendCommand: WaveEnv["rpc"]["AiAgentSendCommand"];
        AiAgentStopCommand: WaveEnv["rpc"]["AiAgentStopCommand"];
    };
    atoms: {
        staticTabId: WaveEnv["atoms"]["staticTabId"];
    };
    wos: WaveEnv["wos"];
}>;
