#!/usr/bin/env python3
"""Test formation offsets — connect, start solo, check unit positions in snapshots."""
import websocket, json, time, struct, sys

ws = websocket.create_connection('ws://localhost:9091/ws')

# Send join
ws.send(json.dumps({'type':'join','name':'FormationBot'}))
time.sleep(0.5)

# Send start_solo with LI commander
ws.send(json.dumps({'type':'start_solo','cmdType':0}))
time.sleep(2)

# Read messages and look for snapshot data with units
unit_positions = []
for i in range(20):
    try:
        ws.set_timeout(1)
        data = ws.recv()
        if isinstance(data, bytes):
            msg_type = data[0]
            if msg_type == 0x01:  # snapshot
                tick = struct.unpack_from('<I', data, 1)[0]
                print(f'Snapshot tick={tick}, bytes={len(data)}')
                
                # Parse snapshot to find unit positions
                # Snapshot format: tick(4) + entity data
                # Each entity: entityId(4) + x(4) + y(4) + flags
                offset = 5
                entities = []
                while offset + 12 <= len(data):
                    eid = struct.unpack_from('<I', data, offset)[0]
                    x = struct.unpack_from('<f', data, offset + 4)[0]
                    y = struct.unpack_from('<f', data, offset + 8)[0]
                    entities.append((eid, x, y))
                    offset += 12
                    # Look for fog marker 0xFF
                    if offset < len(data) and data[offset] == 0xFF:
                        break
                
                if entities:
                    print(f'  Found {len(entities)} entities:')
                    xs = [e[1] for e in entities]
                    ys = [e[2] for e in entities]
                    for eid, x, y in entities:
                        print(f'    entity {eid}: x={x:.2f}, y={y:.2f}')
                    
                    # Check spread
                    if len(xs) > 1:
                        x_range = max(xs) - min(xs)
                        y_range = max(ys) - min(ys)
                        print(f'  X spread: {x_range:.2f}, Y spread: {y_range:.2f}')
                        
                        if x_range < 0.1 and y_range < 0.1:
                            print('  BUG: All units at same position!')
                        else:
                            print('  OK: Units are spread out')
                    break
            else:
                print(f'MSG {i}: binary type=0x{msg_type:02x}, {len(data)} bytes')
        else:
            print(f'MSG {i}: text: {data[:200]}')
    except websocket.WebSocketTimeoutException:
        print(f'No more messages after {i}')
        break
    except Exception as e:
        print(f'Error: {e}')
        break

ws.close()
