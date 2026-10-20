// tk connector: tag every shell command of the session with the agent identity,
// so tk writes "Actor: opencode:<sessionID>" into its commits.
export const TkActor = async () => ({
  "shell.env": async (input, output) => {
    if (input.sessionID) output.env.TRACKER_ACTOR = `opencode:${input.sessionID}`
  },
})
