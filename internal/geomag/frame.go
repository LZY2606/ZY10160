package geomag

import "math"

// Coordinate frame identifiers.  Every frame is right-handed and orthonormal.
//
//	spec  specimen/measurement frame: X = specimen reference mark,
//	      Y = 90 degrees clockwise from X looking at the marked face,
//	      Z = down along the specimen axis (X cross Y).
//	geo   geographic frame: X = north, Y = east, Z = down.
//	till  tilt-corrected (stratigraphic) frame: bedding restored to
//	      horizontal; X along restored strike reference, Z vertical down.
const (
	FrameSpec = "spec"
	FrameGeo  = "geo"
	FrameTilt = "tilt"
)

// Attitude describes how a specimen was mounted when a step was measured.
// All angles are in degrees.
//
//	Azimuth   direction (in geographic coordinates, clockwise from north)
//	          toward which the specimen +X reference mark points after
//	          mounting.  Range 0..360.
//	Plunge    plunge of the specimen +X axis, positive downward.
//	Roll      rotation of the specimen about its own +X axis applied after
//	          azimuth/plunge; positive per the right-hand rule about +X
//	          (+Y tilts toward +Z).
type Attitude struct {
	Azimuth float64
	Plunge  float64
	Roll    float64
}

// Bedding describes a bedding plane used for tectonic tilt correction.
//
//	Strike  azimuth of the strike line, clockwise from north (degrees).
//	Dip     inclination of the plane, positive downward toward the
//	        dip-direction which is strike+90 degrees (degrees, 0..90).
type Bedding struct {
	Strike float64
	Dip    float64
}

// SpecToGeo builds the rotation taking specimen-frame vectors into the
// geographic frame for the given mounting attitude: v_geo = R * v_spec.
//
// Construction (intrinsic rotations, applied right to left):
//  1. Roll about the specimen +X axis,
//  2. Plunge about Y (pitch +X down by Plunge),
//  3. Azimuth about geographic Z (+X from north toward east).
func SpecToGeo(a Attitude) Mat3 {
	rz := RotZ(Deg(a.Azimuth))
	ry := RotY(Deg(a.Plunge))
	rx := RotX(Deg(a.Roll))
	return rz.MulMat(ry).MulMat(rx)
}

// GeoToTilt restores bedding to horizontal.  The untilt rotates the
// geographic frame by -Dip about the strike line (right-hand rule about the
// strike azimuth); a plane dipping in the strike+90 direction is brought
// back to level.  v_tilt = R * v_geo.
func GeoToTilt(b Bedding) Mat3 {
	// Axis u = (cos strike, sin strike, 0) in the NED geographic frame.
	// Rotation by angle theta about u via Rodrigues formula.
	theta := Deg(-b.Dip)
	ux := math.Cos(Deg(b.Strike))
	uy := math.Sin(Deg(b.Strike))
	c := math.Cos(theta)
	sn := math.Sin(theta)
	t := 1 - c
	return Mat3{
		t*ux*ux + c, t * ux * uy, sn * uy,
		t * ux * uy, t*uy*uy + c, -sn * ux,
		-sn * uy, sn * ux, c,
	}
}

// ChainStep records one node of the coordinate-transformation chain so that
// any reported direction can be traced back, level by level.
type ChainStep struct {
	FromFrame string  `json:"from_frame"`
	ToFrame   string  `json:"to_frame"`
	About     string  `json:"about"`
	AngleDeg  float64 `json:"angle_deg"`
	Matrix    Mat3    `json:"matrix"`
}

// TransformChain describes the full transform applied to a measurement.
type TransformChain struct {
	Steps    []ChainStep `json:"steps"`
	Combined Mat3        `json:"combined"`
}

// BuildChain constructs the transform from the measurement frame up to the
// requested target frame, recording every elementary rotation.
func BuildChain(a Attitude, b *Bedding, target string) TransformChain {
	rSpecGeo := SpecToGeo(a)
	chain := TransformChain{Combined: Identity()}

	chain.Steps = append(chain.Steps,
		ChainStep{FrameSpec, FrameGeo, "X(roll)", a.Roll, RotX(Deg(a.Roll))},
		ChainStep{FrameSpec, FrameGeo, "Y(plunge)", a.Plunge, RotY(Deg(a.Plunge))},
		ChainStep{FrameSpec, FrameGeo, "Z(azimuth)", a.Azimuth, RotZ(Deg(a.Azimuth))},
	)
	chain.Combined = rSpecGeo

	if target == FrameTilt {
		if b != nil {
			rGeoTilt := GeoToTilt(*b)
			chain.Steps = append(chain.Steps, ChainStep{
				FrameGeo, FrameTilt, "strike axis (untilt)", -b.Dip, rGeoTilt,
			})
			chain.Combined = rGeoTilt.MulMat(chain.Combined)
		}
	}
	return chain
}

// ApplyChain maps a specimen-frame vector through a recorded chain.
func ApplyChain(c TransformChain, v Vec3) Vec3 { return c.Combined.Mul(v) }
