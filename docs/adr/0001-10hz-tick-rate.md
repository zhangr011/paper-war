# 10 Hz Tick Rate

The server simulation runs at 10 ticks per second (100ms per tick), not the originally specified 15 Hz. 15 Hz was too expensive for the target unit count (500-1000+ at full scale) and 5 Hz (the prototype) made movement visibly choppy. 10 Hz splits the difference: enough per-tick compute budget for large-scale battles, acceptable responsiveness for RTS squad command (not twitch aiming), and manageable bandwidth (10 snapshots/sec instead of 15). Client interpolates between snapshots for 60fps rendering.
