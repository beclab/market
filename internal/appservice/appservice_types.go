package appservice

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ApplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// the entrance of the application
	Index string `json:"index,omitempty"`

	// description from app's description or frontend
	Description string `json:"description,omitempty"`

	// The url of the icon
	Icon string `json:"icon,omitempty"`

	// the name of the application
	Name string `json:"name"`

	// the unique id of the application
	// for sys application appid equal name otherwise appid equal md5(name)[:8]
	Appid string `json:"appid"`

	IsSysApp bool `json:"isSysApp"`

	// the namespace of the application
	Namespace string `json:"namespace,omitempty"`

	// the deployment of the application
	DeploymentName string `json:"deployment,omitempty"`

	// the owner of the application
	Owner string `json:"owner,omitempty"`

	//Entrances []Entrance `json:"entrances,omitempty"`
	Entrances []Entrance `json:"entrances,omitempty"`

	// the extend settings of the application
	Settings map[string]string `json:"settings,omitempty"`
}

type Entrance struct {
	Name      string `yaml:"name" json:"name"`
	Host      string `yaml:"host" json:"host"`
	Port      int32  `yaml:"port" json:"port"`
	Icon      string `yaml:"icon,omitempty" json:"icon,omitempty"`
	Title     string `yaml:"title" json:"title"`
	AuthLevel string `yaml:"authLevel,omitempty" json:"authLevel,omitempty"`
	Invisible bool   `yaml:"invisible,omitempty" json:"invisible,omitempty"`
}

// ApplicationStatus defines the observed state of Application
type ApplicationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// the state of the application: draft, submitted, passed, rejected, suspended, active
	State      string       `json:"state,omitempty"`
	UpdateTime *metav1.Time `json:"updateTime"`
	StatusTime *metav1.Time `json:"statusTime"`
}

type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationSpec   `json:"spec,omitempty"`
	Status ApplicationStatus `json:"status,omitempty"`
}

type ApplicationManagerState string

var (
	Pending      ApplicationManagerState = "pending"
	Installing   ApplicationManagerState = "installing"
	Upgrading    ApplicationManagerState = "upgrading"
	Uninstalling ApplicationManagerState = "uninstalling"
	Canceled     ApplicationManagerState = "canceled"
	Failed       ApplicationManagerState = "failed"
	Completed    ApplicationManagerState = "completed"
	Suspend      ApplicationManagerState = "suspend"

	Processing ApplicationManagerState = "processing"
)

func (a ApplicationManagerState) String() string {
	return string(a)
}

type OpType string

var (
	Install    OpType = "install"
	Uninstall  OpType = "uninstall"
	Upgrade    OpType = "upgrade"
	SuspendApp OpType = "suspend"
	ResumeApp  OpType = "resume"
	Cancel     OpType = "cancel"
)

type OperateResult struct {
	AppName           string                  `json:"appName"`
	AppNamespace      string                  `json:"appNamespace,omitempty"`
	AppOwner          string                  `json:"appOwner,omitempty"`
	State             ApplicationManagerState `json:"state"`
	OpType            OpType                  `json:"opType"`
	Message           string                  `json:"message"`
	ResourceType      string                  `json:"resourceType"`
	CreationTimestamp metav1.Time             `json:"creationTimestamp"`
	Progress          string                  `json:"progress,omitempty"`
}

type ClusterMetrics struct {
	CPU    Value `json:"cpu"`
	Memory Value `json:"memory"`
	Disk   Value `json:"disk"`
	GPU    Value `json:"gpu"`
}

type Value struct {
	Total float64 `json:"total"`
	Usage float64 `json:"usage"`
	Ratio float64 `json:"ratio"`
	Unit  string  `json:"unit"`
}

type StatusResult struct {
	Name              string            `json:"name"`
	AppID             string            `json:"appID"`
	Namespace         string            `json:"namespace"`
	CreationTimestamp metav1.Time       `json:"creationTimestamp"`
	AppStatus         ApplicationStatus `json:"status"`
}

type StatusResultList struct {
	Result []StatusResult `json:"result"`
}

type OperateHistoryResult struct {
	AppName           string      `json:"appName"`
	AppNamespace      string      `json:"appNamespace"`
	AppOwner          string      `json:"appOwner"`
	ResourceType      string      `json:"resourceType"`
	CreationTimestamp metav1.Time `json:"creationTimestamp"`
	OpRecords         []OpRecord  `json:"opRecords"`
}

type OpRecord struct {
	OpType     OpType       `json:"opType"`
	Message    string       `json:"message"`
	StatusTime *metav1.Time `json:"statusTime"`
}

type UserInfoResult struct {
	UserName string `json:"username"`
	Role     string `json:"role"`
}
