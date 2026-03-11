package cluster

import (
	"math"
	"math/rand"
)

// PCA2D projects high-dimensional embeddings to 2D using power iteration
// to find the top 2 eigenvectors of the covariance matrix.
// Returns xs and ys slices of the same length as embeddings.
func PCA2D(embeddings [][]float32) (xs, ys []float64) {
	components := pcaProject(embeddings, 2)
	if components == nil {
		return nil, nil
	}
	return components[0], components[1]
}

// PCA3D projects high-dimensional embeddings to 3D using power iteration
// to find the top 3 eigenvectors of the covariance matrix.
func PCA3D(embeddings [][]float32) (xs, ys, zs []float64) {
	components := pcaProject(embeddings, 3)
	if components == nil {
		return nil, nil, nil
	}
	return components[0], components[1], components[2]
}

// pcaProject projects embeddings onto the top k principal components.
// Returns a slice of k slices, each of length len(embeddings).
func pcaProject(embeddings [][]float32, k int) [][]float64 {
	n := len(embeddings)
	if n == 0 {
		return nil
	}
	dim := len(embeddings[0])
	if dim == 0 {
		result := make([][]float64, k)
		for i := range result {
			result[i] = make([]float64, n)
		}
		return result
	}

	// Convert to float64 and compute mean.
	mean := make([]float64, dim)
	data := make([][]float64, n)
	for i, emb := range embeddings {
		data[i] = make([]float64, dim)
		for j, v := range emb {
			data[i][j] = float64(v)
			mean[j] += float64(v)
		}
	}
	for j := range mean {
		mean[j] /= float64(n)
	}

	// Center the data.
	for i := range data {
		for j := range data[i] {
			data[i][j] -= mean[j]
		}
	}

	// Find principal components via successive deflated power iteration.
	pcs := make([][]float64, k)
	for c := 0; c < k; c++ {
		var deflate [][]float64
		if c > 0 {
			deflate = pcs[:c]
		}
		pcs[c] = powerIteration(data, dim, deflate)
	}

	// Project each point onto each PC.
	result := make([][]float64, k)
	for c := 0; c < k; c++ {
		result[c] = make([]float64, n)
		for i, row := range data {
			for j := range row {
				result[c][i] += row[j] * pcs[c][j]
			}
		}
	}

	return result
}

// powerIteration finds a principal component of centered data using power
// iteration on the implicit covariance matrix (X^T X). Components in the
// directions of deflate vectors are removed each iteration.
func powerIteration(data [][]float64, dim int, deflate [][]float64) []float64 {
	rng := rand.New(rand.NewSource(42))

	// Random initial vector.
	v := make([]float64, dim)
	for j := range v {
		v[j] = rng.NormFloat64()
	}
	normalize(v)

	const maxIter = 200
	for iter := 0; iter < maxIter; iter++ {
		// Multiply by covariance matrix: v_new = X^T (X v)
		// Step 1: Xv (n-vector)
		xv := make([]float64, len(data))
		for i, row := range data {
			for j, val := range row {
				xv[i] += val * v[j]
			}
		}

		// Step 2: X^T (Xv) (dim-vector)
		vNew := make([]float64, dim)
		for i, row := range data {
			for j, val := range row {
				vNew[j] += val * xv[i]
			}
		}

		// Deflate: remove components along previously found eigenvectors.
		for _, d := range deflate {
			dot := 0.0
			for j := range vNew {
				dot += vNew[j] * d[j]
			}
			for j := range vNew {
				vNew[j] -= dot * d[j]
			}
		}

		normalize(vNew)
		v = vNew
	}

	return v
}

// normalize scales a vector to unit length in-place.
func normalize(v []float64) {
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm < 1e-12 {
		return
	}
	for i := range v {
		v[i] /= norm
	}
}
