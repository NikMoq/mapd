using Go = import "/go.capnp";
@0xda3a0d9284ca402f;
$Go.package("offline");
$Go.import("pfeifer.dev/mapd/cereal/offline");

struct Way {
  name @0 :Text;
  ref @1 :Text;
  maxSpeed @2 :Float64;
  minLat @3 :Float64;
  minLon @4 :Float64;
  maxLat @5 :Float64;
  maxLon @6 :Float64;
  nodes @7 :List(Coordinates);
  lanes @8 :UInt8;
  advisorySpeed @9 :Float64;
  hazard @10 :Text;
  oneWay @11 :Bool;
  maxSpeedForward @12 :Float64;
  maxSpeedBackward @13 :Float64;
}

struct Coordinates {
  latitude @0 :Float64;
  longitude @1 :Float64;
}

struct Camera {
  latitude   @0 :Float64;
  longitude  @1 :Float64;
  type       @2 :Text;       # "stationary", "mobile", "tripod", "avtodoria"
  speedLimit @3 :Float32;    # km/h from OSM way, 0 if unknown
  bearing    @4 :Float32;    # 0-360, direction of enforcement
  confidence @5 :Float32;    # 0.0-1.0, map-matching quality
  groupId    @6 :Text;       # for cascade cameras (avtodoria)
  timestamp  @7 :UInt32;     # unix timestamp when added to DB (for mobile TTL)
}

struct CameraTile {
  minLat  @0 :Float64;
  minLon  @1 :Float64;
  maxLat  @2 :Float64;
  maxLon  @3 :Float64;
  cameras @4 :List(Camera);
  hash    @5 :UInt64;        # Morton code / z-order for fast lookup
}

struct Offline {
  minLat  @0 :Float64;
  minLon  @1 :Float64;
  maxLat  @2 :Float64;
  maxLon  @3 :Float64;
  ways    @4 :List(Way);
  overlap @5 :Float64;
  cameraTiles @6 :List(CameraTile);
  generation  @7 :UInt32;    # unix timestamp of generation
  version     @8 :Text;      # e.g. "2026-05-02-v3"
}
