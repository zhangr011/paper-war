#!/usr/bin/env python3
"""Quick fog test — connect to server, inspect fog data in snapshots."""
import websocket, json, time, struct, sys

ws = websocket.create_connection('ws://localhost:9091/ws')

# Send join
ws.send(json.dumps({'action':'join','name':'test'}))
time.sleep(0.5)

# Read messages
snapshots_seen = 0
for i in range(30):
    try:
        ws.settimeout(1.0)
        data = ws.recv()
        if isinstance(data, bytes):
            # Check for fog marker (0xFF 0xFD)
            fog_found = False
            for j in range(len(data)-5, -1, -1):
                if data[j] == 0xFF and j+1 < len(data) and data[j+1] == 0xFD:
                    fogW = struct.unpack_from('<H', data, j+2)[0]
                    fogH = struct.unpack_from('<H', data, j+4)[0]
                    fogSize = fogW * fogH
                    print(f'SNAP #{i}: {len(data)}B, fog at {j}, grid {fogW}x{fogH}={fogSize}')
                    # Count fog states
                    fullFog = data[j+6:j+6+fogSize]
                    counts = {}
                    for b in fullFog:
                        counts[b] = counts.get(b, 0) + 1
                    print(f'  fog states: {counts}')
                    snapshots_seen += 1
                    fog_found = True
                    break
            if not fog_found:
                tick = struct.unpack_from('<I', data, 0)[0] if len(data) >= 4 else -1
                print(f'BIN #{i}: {len(data)}B, tick={tick}, NO fog')
                if len(data) < 20:
                    print(f'  raw: {data.hex()}')
        else:
            print(f'TXT #{i}: {data[:200]}')
            if 'match_found' in data:
                print('  >>> MATCH FOUND')
    except websocket.WebSocketTimeoutException:
        if snapshots_seen > 0:
            print(f'Got {snapshots_seen} snapshots with fog, done.')
            break
        print(f'timeout #{i}')
    except Exception as e:
        print(f'error: {e}')
        break

ws.close()
print('Done.')
