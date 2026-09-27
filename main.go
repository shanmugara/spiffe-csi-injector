package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"spiffe-csi-injector/admission"
	"spiffe-csi-injector/mutation"

	"github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	TLS_CERT  = "/etc/webhook/certs/tls.crt"
	TLS_KEY   = "/etc/webhook/certs/tls.key"
	TLS_PORT  = "8443"
	HTTP_PORT = "8080"
)

// sharedClient is built once at startup (backed by a cache scoped to just
// the extra-env ConfigMap) and reused across every admission request,
// instead of each request building its own throwaway, uncached client.
var sharedClient client.Client

func main() {
	// setup log levels
	setLogger()

	cl, err := newCachedClient()
	if err != nil {
		logrus.Fatalf("failed to set up Kubernetes client: %v", err)
	}
	sharedClient = cl

	// handle the default routes
	http.HandleFunc("/mutate", ServeMutatePods)
	http.HandleFunc("/healthz", ServeHealthz)

	if os.Getenv("TLS_ENABLED") == "true" {
		logger := logrus.WithField("port", TLS_PORT)
		logger.Info("starting server with TLS")
		err := http.ListenAndServeTLS(":"+TLS_PORT, TLS_CERT, TLS_KEY, nil)
		if err != nil {
			logger.Fatalf("failed to start server: %v", err)
		}
	} else {
		logger := logrus.WithField("port", HTTP_PORT)
		logger.Info("starting server without TLS")
		err := http.ListenAndServe(":"+HTTP_PORT, nil)
		if err != nil {
			logger.Fatalf("failed to start server: %v", err)
		}
	}
}

func ServeMutatePods(w http.ResponseWriter, r *http.Request) {
	logger := logrus.WithField("uri", r.RequestURI)
	logger.Debug("received mutation request")

	in, err := parseRequest(*r)
	if err != nil {
		logger.Error(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	logger.Infof("creating admission struct")
	adm := admission.Admitter{
		Client:  sharedClient,
		Logger:  logger,
		Request: in.Request,
	}
	logger.Infof("validating generate request object")
	out, err := adm.MutatePodReview()
	if err != nil {
		e := fmt.Sprintf("could not generate admission response: %v", err)
		logger.Error(e)
		http.Error(w, e, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	logger.Infof("marsahlling response to json")
	jout, err := json.Marshal(out)
	if err != nil {
		e := fmt.Sprintf("could not parse admission response: %v", err)
		logger.Error(e)
		http.Error(w, e, http.StatusInternalServerError)
		return
	}

	logger.Debug("sending response")
	logger.Debugf("%s", jout)
	fmt.Fprintf(w, "%s", jout)
}

// newCachedClient builds a client backed by a SharedInformer-based cache,
// scoped to just the extra-env ConfigMap by name, in this pod's own
// namespace (GetExtraEnvCm always looks it up via POD_NAMESPACE, i.e. the
// injector's own namespace, never the target pod's) - so a plain namespaced
// Role/RoleBinding is all the RBAC this ever needs, matching how it's
// already granted live (no ClusterRole required). Admission requests are
// served from this locally synced cache instead of a live API call each
// time.
func newCachedClient() (client.Client, error) {
	cfg := config.GetConfigOrDie()
	ownNamespace := os.Getenv("POD_NAMESPACE")

	c, err := cache.New(cfg, cache.Options{
		DefaultNamespaces: map[string]cache.Config{
			ownNamespace: {},
		},
		ByObject: map[client.Object]cache.ByObject{
			&corev1.ConfigMap{}: {
				Field: fields.OneTermEqualSelector("metadata.name", mutation.ExtraEnvCmName),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build cache: %w", err)
	}

	ctx := context.Background()
	go func() {
		if err := c.Start(ctx); err != nil {
			logrus.Fatalf("cache stopped unexpectedly: %v", err)
		}
	}()
	if !c.WaitForCacheSync(ctx) {
		return nil, fmt.Errorf("failed to sync cache")
	}

	return client.New(cfg, client.Options{
		Cache: &client.CacheOptions{Reader: c},
	})
}

func ServeHealthz(w http.ResponseWriter, r *http.Request) {
	logrus.WithField("uri", r.RequestURI).Debug("received health check request")
	fmt.Fprint(w, "ok")

}

// parseRequest extracts an AdmissionReview from an http.Request if possible
func parseRequest(r http.Request) (*admissionv1.AdmissionReview, error) {
	logger := logrus.New()
	logger.Infof("parsing request parseRequest")
	contentType := r.Header.Get("Content-Type")

	if contentType != "application/json" {
		return nil, fmt.Errorf("Content-Type: %q should be %q",
			contentType, "application/json")
	}

	bodybuf := new(bytes.Buffer)
	_, err := bodybuf.ReadFrom(r.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read request body: %v", err)
	}
	body := bodybuf.Bytes()

	if len(body) == 0 {
		return nil, fmt.Errorf("admission request body is empty")
	}

	var a admissionv1.AdmissionReview

	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("could not parse admission review request: %v", err)
	}

	//if the admissionreview request is nil, return an error
	if a.Request == nil {
		return nil, fmt.Errorf("admission review can't be used: Request field is nil")
	}

	return &a, nil
}

// setLogger sets the logger using env vars, it defaults to text logs on
// debug level unless otherwise specified
func setLogger() {
	// Set the default log level
	logrus.SetLevel(logrus.DebugLevel)

	// Set the log level from the LOG_LEVEL environment variable if set
	lev := os.Getenv("LOG_LEVEL")
	if lev != "" {
		llev, err := logrus.ParseLevel(lev)
		if err != nil {
			logrus.Fatalf("cannot set LOG_LEVEL to %q", lev)
		}
		logrus.SetLevel(llev)
	}

	if os.Getenv("LOG_JSON") == "true" {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	}
}
